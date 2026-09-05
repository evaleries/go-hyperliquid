package hyperliquid

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/vmihailenco/msgpack/v5"
)

// actionEncoder pairs a pooled msgpack buffer with its encoder so a single
// Get/Put covers both and exactly one place owns the encoder options.
//
// CRITICAL: msgpack's Encoder.Reset zeroes every encoder flag (v5
// encode.go: ResetDict sets e.flags = 0), so UseCompactInts MUST be
// re-armed after each Reset. Without it, sized integer fields (int64,
// *int64, any-boxed int64 — e.g. CancelOrderWire.OrderID,
// ScheduleCancelAction.Time, ModifyAction.Oid) encode full-width instead of
// compact, which changes the action hash and therefore the signature.
// Plain `int` fields are compact unconditionally, which is why order
// placement is insensitive to the flag but cancel/modify are not.
type actionEncoder struct {
	buf *bytes.Buffer
	enc *msgpack.Encoder
}

// actionEncoderPool recycles action encoders; 256 bytes covers typical action
// payloads without regrowth.
var actionEncoderPool = sync.Pool{New: func() any {
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	return &actionEncoder{buf: buf, enc: msgpack.NewEncoder(buf)}
}}

// getActionEncoder returns a pooled encoder with a cleared buffer and the
// Python-compatible options armed.
func getActionEncoder() *actionEncoder {
	ae := actionEncoderPool.Get().(*actionEncoder)
	ae.buf.Reset()
	ae.enc.Reset(ae.buf)
	ae.enc.UseCompactInts(true) // Reset() cleared it — see actionEncoder
	// CRITICAL: Do NOT use SetSortMapKeys(true) - Python preserves insertion order
	// Structs in Go will serialize fields in the order they are defined
	return ae
}

func actionHash(
	action any,
	vaultAddress string,
	nonce int64,
	expiresAfter *int64,
) ([32]byte, error) {
	ae := getActionEncoder()
	defer actionEncoderPool.Put(ae)

	if err := ae.enc.Encode(action); err != nil {
		return [32]byte{}, fmt.Errorf("failed to marshal action: %w", err)
	}

	// The msgpack bytes go into the hash as produced: the pinned encoder emits
	// str8 headers for strings shorter than 256 bytes, matching Python's
	// msgpack, so no header rewriting is needed
	// (TestMsgpackStringHeadersStayPythonCompatible pins this). The trailer
	// appended below (8-byte nonce + up to 1+20 vault + 1+8 expires) fits in
	// the pooled buffer's spare capacity, which is cleared on the next Get.
	data := ae.buf.Bytes()

	// Add nonce as 8 bytes big endian
	if nonce < 0 {
		return [32]byte{}, fmt.Errorf("nonce cannot be negative: %d", nonce)
	}
	data = binary.BigEndian.AppendUint64(data, uint64(nonce))

	// Add vault address
	if vaultAddress == "" {
		data = append(data, 0x00)
	} else {
		addr, err := decodeVaultAddress(vaultAddress)
		if err != nil {
			return [32]byte{}, err
		}
		data = append(data, 0x01)
		data = append(data, addr[:]...)
	}

	// Add expires_after if provided
	if expiresAfter != nil {
		if *expiresAfter < 0 {
			return [32]byte{}, fmt.Errorf("expiresAfter cannot be negative: %d", *expiresAfter)
		}
		data = append(data, 0x00)
		data = binary.BigEndian.AppendUint64(data, uint64(*expiresAfter))
	}

	// Return keccak256 hash (value return: no []byte heap allocation)
	return crypto.Keccak256Hash(data), nil
}

// decodeVaultAddress decodes a 20-byte vault address into a stack array.
// The length is validated first: hex.Decode writes len(src)/2 bytes and
// panics when the destination is too short, and a mistyped address must
// surface as an error rather than a panic or a silently zero-padded address
// that would end up inside a signed payload.
func decodeVaultAddress(vaultAddress string) ([20]byte, error) {
	var addr [20]byte
	hexPart := strings.TrimPrefix(vaultAddress, "0x")
	if len(hexPart) != 2*len(addr) {
		return addr, fmt.Errorf(
			"vault address must be %d hex characters (got %d): %q",
			2*len(addr), len(hexPart), vaultAddress,
		)
	}
	if _, err := hex.Decode(addr[:], []byte(hexPart)); err != nil {
		return addr, fmt.Errorf("invalid vault address %q: %w", vaultAddress, err)
	}
	return addr, nil
}

// SignatureResult represents the structured signature result
type SignatureResult struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

// L1ActionSigner signs L1 actions (msgpack + phantom agent EIP-712).
// When nil on Exchange, the default ECDSA implementation is used.
type L1ActionSigner interface {
	SignL1Action(
		ctx context.Context,
		action any,
		vaultAddress string,
		timestamp int64,
		expiresAfter *int64,
		isMainnet bool,
	) (SignatureResult, error)
}

// UserSignedActionSigner signs direct EIP-712 user-signed actions.
// When nil on Exchange, the default ECDSA implementation is used.
type UserSignedActionSigner interface {
	SignUserSignedAction(
		ctx context.Context,
		action map[string]any,
		payloadTypes []apitypes.Type,
		primaryType string,
		isMainnet bool,
	) (SignatureResult, error)
}

// AgentSigner signs agent approval actions.
// When nil on Exchange, the default ECDSA implementation is used.
type AgentSigner interface {
	SignAgent(
		ctx context.Context,
		agentAddress, agentName string,
		nonce int64,
		isMainnet bool,
	) (SignatureResult, error)
}

// ECDSAL1Signer implements L1ActionSigner using an ECDSA private key.
func ECDSAL1Signer(pk *ecdsa.PrivateKey) L1ActionSigner {
	return &ecdsaL1Signer{pk: pk}
}

type ecdsaL1Signer struct{ pk *ecdsa.PrivateKey }

func (s *ecdsaL1Signer) SignL1Action(
	_ context.Context,
	action any,
	vaultAddress string,
	timestamp int64,
	expiresAfter *int64,
	isMainnet bool,
) (SignatureResult, error) {
	return SignL1Action(s.pk, action, vaultAddress, timestamp, expiresAfter, isMainnet)
}

// ECDSAUserSignedSigner implements UserSignedActionSigner using an ECDSA private key.
func ECDSAUserSignedSigner(pk *ecdsa.PrivateKey) UserSignedActionSigner {
	return &ecdsaUserSignedSigner{pk: pk}
}

type ecdsaUserSignedSigner struct{ pk *ecdsa.PrivateKey }

func (s *ecdsaUserSignedSigner) SignUserSignedAction(
	_ context.Context,
	action map[string]any,
	payloadTypes []apitypes.Type,
	primaryType string,
	isMainnet bool,
) (SignatureResult, error) {
	return SignUserSignedAction(s.pk, action, payloadTypes, primaryType, isMainnet)
}

// ECDSAAgentSigner implements AgentSigner using an ECDSA private key.
func ECDSAAgentSigner(pk *ecdsa.PrivateKey) AgentSigner {
	return &ecdsaAgentSigner{pk: pk}
}

type ecdsaAgentSigner struct{ pk *ecdsa.PrivateKey }

func (s *ecdsaAgentSigner) SignAgent(
	_ context.Context,
	agentAddress, agentName string,
	nonce int64,
	isMainnet bool,
) (SignatureResult, error) {
	return SignAgent(s.pk, agentAddress, agentName, nonce, isMainnet)
}

// hashStructLenient is like HashStruct but ignores fields in message that are not in types
// This matches Python's eth_account behavior where extra fields in message are silently ignored
func hashStructLenient(
	typedData apitypes.TypedData,
	primaryType string,
	message map[string]any,
) ([]byte, error) {
	types := typedData.Types[primaryType]

	// Filter message to only include fields that exist in type definition
	// Also convert numeric types to ensure proper type handling for EIP-712
	filteredMessage := make(map[string]any, len(types))
	for _, t := range types {
		if val, ok := message[t.Name]; ok {
			// Convert numeric types to ensure proper type handling for EIP-712
			// apitypes.HashStruct expects specific types based on the type declaration
			switch t.Type {
			case "uint64":
				var uintVal uint64
				switch v := val.(type) {
				case uint64:
					uintVal = v
				case int64:
					if v < 0 {
						return nil, fmt.Errorf("cannot convert negative int64 %d to uint64", v)
					}
					uintVal = uint64(v)
				case float64:
					// JSON unmarshaling can convert numbers to float64
					if v < 0 || v > float64(^uint64(0)) || v != float64(uint64(v)) {
						return nil, fmt.Errorf("invalid float64 value %f for uint64", v)
					}
					uintVal = uint64(v)
				case int:
					if v < 0 {
						return nil, fmt.Errorf("cannot convert negative int %d to uint64", v)
					}
					uintVal = uint64(v)
				case json.Number:
					// Handle json.Number type
					parsed, err := strconv.ParseUint(string(v), 10, 64)
					if err != nil {
						return nil, fmt.Errorf(
							"failed to parse json.Number %s to uint64 for %s: %w",
							v,
							t.Name,
							err,
						)
					}
					uintVal = parsed
				case string:
					// Try to parse as string representation of uint64
					parsed, err := strconv.ParseUint(v, 10, 64)
					if err != nil {
						return nil, fmt.Errorf(
							"failed to parse string %s to uint64 for %s: %w",
							v,
							t.Name,
							err,
						)
					}
					uintVal = parsed
				default:
					// Try to convert via json marshal/unmarshal to handle edge cases
					jsonBytes, err := jsonCodec.Marshal(v)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal value for %s: %w", t.Name, err)
					}
					if err := jsonCodec.Unmarshal(jsonBytes, &uintVal); err != nil {
						return nil, fmt.Errorf(
							"failed to convert value to uint64 for %s: %w",
							t.Name,
							err,
						)
					}
				}
				// apitypes.HashStruct may not handle uint64 directly from map[string]any
				// Convert to *big.Int which is commonly used for EIP-712 uint types
				filteredMessage[t.Name] = new(big.Int).SetUint64(uintVal)
			default:
				filteredMessage[t.Name] = val
			}
		}
	}

	// Now use standard HashStruct with filtered message
	return typedData.HashStruct(primaryType, filteredMessage)
}

// The EIP-712 domains used by Hyperliquid are constant, so their separators
// are computed once at package init instead of on every signature. This
// removes a per-call map allocation (TypedDataDomain.Map), a reflection-based
// HashStruct and a keccak round from the hot path.
var (
	l1Domain = apitypes.TypedDataDomain{
		Name:              "Exchange",
		Version:           "1",
		ChainId:           (*math.HexOrDecimal256)(big.NewInt(1337)),
		VerifyingContract: "0x0000000000000000000000000000000000000000",
	}
	userSignedDomain = apitypes.TypedDataDomain{
		Name:              "HyperliquidSignTransaction",
		Version:           "1",
		ChainId:           (*math.HexOrDecimal256)(big.NewInt(421614)),
		VerifyingContract: "0x0000000000000000000000000000000000000000",
	}

	l1DomainSeparator         = computeDomainSeparator(l1Domain)
	userSignedDomainSeparator = computeDomainSeparator(userSignedDomain)
)

// computeDomainSeparator hashes a constant EIP-712 domain using the generic
// apitypes machinery once, guaranteeing byte-identical output with the
// previous per-call implementation.
func computeDomainSeparator(domain apitypes.TypedDataDomain) []byte {
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
		},
		Domain: domain,
	}
	sep, err := typedData.HashStruct("EIP712Domain", domain.Map())
	if err != nil {
		panic(fmt.Sprintf("failed to hash constant EIP-712 domain %q: %v", domain.Name, err))
	}
	return sep
}

// EIP-712 struct hash precomputation for the fixed L1 "Agent" schema:
//
//	Agent(string source,bytes32 connectionId)
//
// keccak256(typeHash ‖ keccak256(source) ‖ connectionId) — identical to
// apitypes.HashStruct but without maps/reflection on the hot path.
var (
	agentTypeHash          = crypto.Keccak256([]byte("Agent(string source,bytes32 connectionId)"))
	agentSourceHashMainnet = crypto.Keccak256([]byte("a"))
	agentSourceHashTestnet = crypto.Keccak256([]byte("b"))
)

func agentStructHash(connectionId []byte, isMainnet bool) [32]byte {
	sourceHash := agentSourceHashTestnet
	if isMainnet {
		sourceHash = agentSourceHashMainnet
	}
	var buf [96]byte
	copy(buf[0:32], agentTypeHash)
	copy(buf[32:64], sourceHash)
	copy(buf[64:96], connectionId)
	return crypto.Keccak256Hash(buf[:])
}

func signInner(
	privateKey *ecdsa.PrivateKey,
	domainSeparator []byte,
	typedData apitypes.TypedData,
) (SignatureResult, error) {
	// Use lenient hashing to allow extra fields in message (Python compatibility)
	typedDataHash, err := hashStructLenient(typedData, typedData.PrimaryType, typedData.Message)
	if err != nil {
		return SignatureResult{}, fmt.Errorf("failed to hash typed data: %w", err)
	}

	return signTypedDataHash(privateKey, domainSeparator, typedDataHash)
}

// signTypedDataHash signs keccak256(0x19 0x01 ‖ domainSeparator ‖ structHash).
func signTypedDataHash(
	privateKey *ecdsa.PrivateKey,
	domainSeparator, structHash []byte,
) (SignatureResult, error) {
	rawData := make([]byte, 2, 66)
	rawData[0] = 0x19
	rawData[1] = 0x01
	rawData = append(rawData, domainSeparator...)
	rawData = append(rawData, structHash...)
	msgHash := crypto.Keccak256Hash(rawData)

	signature, err := crypto.Sign(msgHash.Bytes(), privateKey)
	if err != nil {
		return SignatureResult{}, fmt.Errorf("failed to sign message: %w", err)
	}

	// Extract r, s, v components. R/S are formatted as 0x-prefixed minimal hex,
	// byte-identical to hexutil.EncodeBig but without the big.Int round-trip.
	return SignatureResult{
		R: hexEncodeBigEndian(signature[:32]),
		S: hexEncodeBigEndian(signature[32:64]),
		V: int(signature[64]) + 27,
	}, nil
}

// hexEncodeBigEndian formats a big-endian byte slice as 0x-prefixed hex with
// no leading zeros ("0x0" for zero), byte-identical to hexutil.EncodeBig.
// strings.Builder.Grow makes it a single allocation with a zero-copy String().
func hexEncodeBigEndian(b []byte) string {
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	if i == len(b) {
		return "0x0"
	}

	var sb strings.Builder
	sb.Grow(2 + (len(b)-i)*2)
	sb.WriteString("0x")
	const digits = "0123456789abcdef"
	if first := b[i]; first >= 16 {
		sb.WriteByte(digits[first>>4])
	}
	sb.WriteByte(digits[b[i]&0xf])
	for _, c := range b[i+1:] {
		sb.WriteByte(digits[c>>4])
		sb.WriteByte(digits[c&0xf])
	}
	return sb.String()
}

// SignUserSignedAction signs actions that require direct EIP-712 signing
// (e.g., approveAgent, approveBuilderFee, convertToMultiSigUser)
//
// IMPORTANT: The message will contain MORE fields than declared in payloadTypes to avoid the error
// "422 Failed to deserialize the JSON body" and "User or API Wallet 0x123... does not exist".
// This matches Python SDK behavior where the field order doesn't matter and extra fields (type, signatureChainId)
// are present in the message but ignored during EIP-712 hashing via hashStructLenient.
func SignUserSignedAction(
	privateKey *ecdsa.PrivateKey,
	action map[string]any,
	payloadTypes []apitypes.Type,
	primaryType string,
	isMainnet bool,
) (SignatureResult, error) {
	// Add signatureChainId based on environment
	// signatureChainId is the chain used by the wallet to sign.
	// hyperliquidChain determines the environment and prevents replay attacks.
	action["signatureChainId"] = "0x66eee"
	action["hyperliquidChain"] = "Mainnet"
	if !isMainnet {
		action["hyperliquidChain"] = "Testnet"
	}

	// Create typed data. The EIP-712 domain (chainId 421614, like the Python
	// SDK) is constant: its separator is precomputed in
	// userSignedDomainSeparator and the shared userSignedDomain is assigned
	// read-only (geth's validate() requires a non-empty domain).
	typedData := apitypes.TypedData{
		Domain: userSignedDomain,
		Types: apitypes.Types{
			primaryType: payloadTypes,
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
		},
		PrimaryType: primaryType,
		Message:     action,
	}

	// signInner uses hashStructLenient which filters message to only include
	// fields declared in payloadTypes, matching Python eth_account behavior
	return signInner(privateKey, userSignedDomainSeparator, typedData)
}

func SignL1Action(
	privateKey *ecdsa.PrivateKey,
	action any,
	vaultAddress string,
	timestamp int64,
	expiresAfter *int64,
	isMainnet bool,
) (SignatureResult, error) {
	// Step 1: Create action hash
	hash, err := actionHash(action, vaultAddress, timestamp, expiresAfter)
	if err != nil {
		return SignatureResult{}, fmt.Errorf("failed to hash action: %w", err)
	}

	// Step 2: EIP-712 struct hash of the phantom agent (fixed "Agent" schema,
	// precomputed type/source hashes — no maps or reflection per call).
	structHash := agentStructHash(hash[:], isMainnet)

	// Step 3: Sign using EIP-712 with the precomputed constant domain separator
	return signTypedDataHash(privateKey, l1DomainSeparator, structHash[:])
}

type signUsdClassTransferAction struct {
	Type   string  `msgpack:"type"`
	Amount float64 `msgpack:"amount"`
	ToPerp bool    `msgpack:"toPerp"`
}

// SignUsdClassTransferAction signs USD class transfer action
func SignUsdClassTransferAction(
	privateKey *ecdsa.PrivateKey,
	amount float64,
	toPerp bool,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signUsdClassTransferAction{
		Type:   "usdClassTransfer",
		Amount: amount,
		ToPerp: toPerp,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signSpotTransferAction struct {
	Type        string  `msgpack:"type"`
	Amount      float64 `msgpack:"amount"`
	Destination string  `msgpack:"destination"`
	Token       string  `msgpack:"token"`
}

// SignSpotTransferAction signs spot transfer action
func SignSpotTransferAction(
	privateKey *ecdsa.PrivateKey,
	amount float64,
	destination, token string,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signSpotTransferAction{
		Type:        "spotTransfer",
		Amount:      amount,
		Destination: destination,
		Token:       token,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signUsdTransferAction struct {
	Type        string  `msgpack:"type"`
	Amount      float64 `msgpack:"amount"`
	Destination string  `msgpack:"destination"`
}

// SignUsdTransferAction signs USD transfer action
func SignUsdTransferAction(
	privateKey *ecdsa.PrivateKey,
	amount float64,
	destination string,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signUsdTransferAction{
		Type:        "usdTransfer",
		Amount:      amount,
		Destination: destination,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signPerpDexClassTransferAction struct {
	Type   string  `msgpack:"type"`
	Dex    string  `msgpack:"dex"`
	Token  string  `msgpack:"token"`
	Amount float64 `msgpack:"amount"`
	ToPerp bool    `msgpack:"toPerp"`
}

// SignPerpDexClassTransferAction signs perp dex class transfer action
func SignPerpDexClassTransferAction(
	privateKey *ecdsa.PrivateKey,
	dex, token string,
	amount float64,
	toPerp bool,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signPerpDexClassTransferAction{
		Type:   "perpDexClassTransfer",
		Dex:    dex,
		Token:  token,
		Amount: amount,
		ToPerp: toPerp,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signTokenDelegateAction struct {
	Type             string  `msgpack:"type"`
	Token            string  `msgpack:"token"`
	Amount           float64 `msgpack:"amount"`
	ValidatorAddress string  `msgpack:"validatorAddress"`
}

// SignTokenDelegateAction signs token delegate action
func SignTokenDelegateAction(
	privateKey *ecdsa.PrivateKey,
	token string,
	amount float64,
	validatorAddress string,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signTokenDelegateAction{
		Type:             "tokenDelegate",
		Token:            token,
		Amount:           amount,
		ValidatorAddress: validatorAddress,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signWithdrawFromBridgeAction struct {
	Type        string  `msgpack:"type"`
	Destination string  `msgpack:"destination"`
	Amount      float64 `msgpack:"amount"`
	Fee         float64 `msgpack:"fee"`
}

// SignWithdrawFromBridgeAction signs withdraw from bridge action
func SignWithdrawFromBridgeAction(
	privateKey *ecdsa.PrivateKey,
	destination string,
	amount, fee float64,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signWithdrawFromBridgeAction{
		Type:        "withdrawFromBridge",
		Destination: destination,
		Amount:      amount,
		Fee:         fee,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

// SignAgent signs agent approval action using EIP-712 direct signing
func SignAgent(
	privateKey *ecdsa.PrivateKey,
	agentAddress, agentName string,
	nonce int64,
	isMainnet bool,
) (SignatureResult, error) {
	// The nonce must be non-negative
	if nonce < 0 {
		return SignatureResult{}, fmt.Errorf("nonce cannot be negative: %d", nonce)
	}

	// Use int64 in the action map - apitypes will handle the conversion to uint64
	// based on the type declaration in payloadTypes
	action := map[string]any{
		"type":         "approveAgent",
		"agentAddress": agentAddress,
		"agentName":    agentName,
		"nonce":        nonce,
	}

	// payload_types from Python: only declares fields that are in the original action
	// signatureChainId and hyperliquidChain are added by SignUserSignedAction
	// but they're NOT declared in payloadTypes (they're added to message dynamically)
	payloadTypes := []apitypes.Type{
		{Name: "hyperliquidChain", Type: "string"},
		{Name: "agentAddress", Type: "address"},
		{Name: "agentName", Type: "string"},
		{Name: "nonce", Type: "uint64"},
	}

	return SignUserSignedAction(
		privateKey,
		action,
		payloadTypes,
		"HyperliquidTransaction:ApproveAgent",
		isMainnet,
	)
}

type signApproveBuilderFee struct {
	Type string `msgpack:"type"`
	// BuilderAddress is the address of the builder
	BuilderAddress string `msgpack:"builderAddress"`
	// MaxFeeRate is the maximum fee rate the user is willing to pay
	MaxFeeRate float64 `msgpack:"maxFeeRate"`
}

// SignApproveBuilderFee signs approve builder fee action
func SignApproveBuilderFee(
	privateKey *ecdsa.PrivateKey,
	builderAddress string,
	maxFeeRate float64,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signApproveBuilderFee{
		Type:           "approveBuilderFee",
		BuilderAddress: builderAddress,
		MaxFeeRate:     maxFeeRate,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signConvertToMultiSigUserAction struct {
	Type      string   `msgpack:"type"`
	Signers   []string `msgpack:"signers"`
	Threshold int      `msgpack:"threshold"`
}

// SignConvertToMultiSigUserAction signs convert to multi-sig user action
func SignConvertToMultiSigUserAction(
	privateKey *ecdsa.PrivateKey,
	signers []string,
	threshold int,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signConvertToMultiSigUserAction{
		Type:      "convertToMultiSigUser",
		Signers:   signers,
		Threshold: threshold,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

type signMultiSigAction struct {
	Type       string         `msgpack:"type"`
	Action     map[string]any `msgpack:"action"`
	Signers    []string       `msgpack:"signers"`
	Signatures []string       `msgpack:"signatures"`
}

// SignMultiSigAction signs multi-signature action
func SignMultiSigAction(
	privateKey *ecdsa.PrivateKey,
	innerAction map[string]any,
	signers []string,
	signatures []string,
	timestamp int64,
	isMainnet bool,
) (SignatureResult, error) {
	action := signMultiSigAction{
		Type:       "multiSig",
		Action:     innerAction,
		Signers:    signers,
		Signatures: signatures,
	}

	return SignL1Action(privateKey, action, "", timestamp, nil, isMainnet)
}

// FloatToUsdInt converts float to USD integer representation
func FloatToUsdInt(value float64) int {
	// Convert float USD to integer representation (assuming 6 decimals for USDC)
	return int(value * 1e6)
}

// GetTimestampMs returns current timestamp in milliseconds
func GetTimestampMs() int64 {
	return time.Now().UnixMilli()
}
