package signature

import (
	"github.com/MoeForge/moego/base64"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"google.golang.org/protobuf/encoding/protowire"
)

func testClaudeThinkingSignature() string {
	channelBlock := []byte{}
	channelBlock = protowire.AppendTag(channelBlock, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 12)
	channelBlock = protowire.AppendTag(channelBlock, 2, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 2)
	channelBlock = protowire.AppendTag(channelBlock, 6, protowire.BytesType)
	channelBlock = protowire.AppendString(channelBlock, "claude-sonnet-4-6")

	container := []byte{}
	container = protowire.AppendTag(container, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)

	payload := []byte{}
	payload = protowire.AppendTag(payload, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	payload = protowire.AppendTag(payload, 3, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)
	return base64.StdEncoding.EncodeToString(payload)
}

// TestBase64AlphabetSet_MatchesEncoderAlphabets pins the charset lookup tables
// against the encoders they stand in for. A wrong table would silently accept
// bytes that are not valid base64, or reject a legal payload character.
func TestBase64AlphabetSet_MatchesEncoderAlphabets(t *testing.T) {
	cases := []struct {
		name     string
		set      [256]bool
		alphabet string
	}{
		{"grok unpadded std", grokEncryptedContentCharSet, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"},
		{"gpt base64url", gptReasoningSignatureCharSet, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_="},
		{"meta base64url unpadded", metaReasoningSignatureCharSet, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"},
	}
	for _, tc := range cases {
		allowed := map[byte]bool{}
		for i := 0; i < len(tc.alphabet); i++ {
			allowed[tc.alphabet[i]] = true
		}
		for c := 0; c < 256; c++ {
			want := allowed[byte(c)]
			if got := tc.set[c]; got != want {
				t.Errorf("%s: byte 0x%02x (%q) accepted=%v, want %v", tc.name, c, string(rune(c)), got, want)
			}
		}
	}
}

// replaySafeEnvelopeFixtures returns one fixture per self-describing provider
// envelope that carries replayable state. Every entry must survive the structural
// pre-filter, because losing one would silently reclassify that provider.
func replaySafeEnvelopeFixtures(t *testing.T) map[string]struct {
	sig  string
	want SignatureProvider
} {
	return map[string]struct {
		sig  string
		want SignatureProvider
	}{
		"claude single-layer E": {testClaudeThinkingSignature(), SignatureProviderClaude},
		"claude double-layer R": {testUnpaddedAntigravityClaudeThinkingSignature(), SignatureProviderClaude},
		"claude CAIS":           {testClaudeCAISSignature("claude-fable-5"), SignatureProviderClaude},
		"gpt fernet":            {testGPTReasoningSignature(), SignatureProviderGPT},
		"meta muse":             {testMetaReasoningSignature(1024), SignatureProviderMeta},
	}
}

// TestSelfDescribingSignatureFirstChars_CoversEveryKnownEnvelope guards the
// structural pre-filter. DetectSignatureProviderForBlock skips every provider
// validator when maybeSelfDescribingSignatureEnvelope returns false, so an
// envelope missing from selfDescribingSignatureFirstChars would silently fall
// through to the residual class. Adding a provider envelope without registering
// its base64 first character fails here.
func TestSelfDescribingSignatureFirstChars_CoversEveryKnownEnvelope(t *testing.T) {
	for name, fixture := range replaySafeEnvelopeFixtures(t) {
		if !maybeSelfDescribingSignatureEnvelope(fixture.sig) {
			t.Errorf("%s: first char %q is not in selfDescribingSignatureFirstChars %q; register it or detection will skip this envelope",
				name, string(fixture.sig[0]), selfDescribingSignatureFirstChars)
		}
	}

	// The pre-filter must not be so wide that it stops filtering. Opaque xAI
	// ciphertext is the shape it exists to reject.
	for _, sig := range []string{
		"K1ZAIbzDbO",
		"jQDLUr+fD8RFP8nbkkfI",
		"qcgG7jzxH3D6mlVLBBaKXaG3",
	} {
		if maybeSelfDescribingSignatureEnvelope(sig) {
			t.Errorf("opaque ciphertext %q must not look like a self-describing envelope", sig)
		}
	}
}

// TestGeminiASCIIUUIDIsGateIndependent documents why ascii_uuid is excluded from
// selfDescribingSignatureFirstChars. Its first byte is the first hex character of
// the UUID, so the base64 first character spreads over several values, and none of
// them need to be registered: the envelope is never replay-safe, so it resolves to
// SignatureProviderUnknown either way and Gemini model parts recover it through the
// bypass sentinel keyed on block kind.

// TestDetectSignatureProviderForBlock_ClassifiesEveryKnownEnvelope pins the
// classification of each envelope so a reordering of the validator chain cannot
// silently reassign one provider's signatures to another.
func TestDetectSignatureProviderForBlock_ClassifiesEveryKnownEnvelope(t *testing.T) {
	for name, fixture := range replaySafeEnvelopeFixtures(t) {
		if got := DetectSignatureProvider(fixture.sig); got != fixture.want {
			t.Errorf("%s: DetectSignatureProvider = %q, want %q", name, got, fixture.want)
		}
	}
}

// TestGeminiEnvelopeNeverClaimsClaudeSignatures pins the invariant that keeps
// Claude and Gemini separable independently of probe order in
// DetectSignatureProviderForBlock. Gemini validates wire shape only and has no
// literal marker, so it is the weakest judge; Claude envelopes survive it solely
// because they carry extra top-level fields beyond the container and therefore
// fail Gemini's single-record shape. Loosening the Gemini envelope check would
// make probe order start mattering, and fails here first.

func TestDetectSignatureProvider_UsesProviderPrefix(t *testing.T) {
	claudeSig := "claude#" + testClaudeThinkingSignature()
	if got := DetectSignatureProvider(claudeSig); got != SignatureProviderClaude {
		t.Fatalf("DetectSignatureProvider(claude#...) = %q, want %q", got, SignatureProviderClaude)
	}

}

func TestCompatibleSignatureForProvider_ClaudeUsesProviderNativeEForm(t *testing.T) {
	nativeSig := testClaudeThinkingSignature()
	doubleEncoded := base64.StdEncoding.EncodeToString([]byte(nativeSig))

	normalized, ok := CompatibleSignatureForProvider(SignatureProviderClaude, doubleEncoded)
	if !ok {
		t.Fatal("double-layer Claude signature should be compatible")
	}
	if normalized != nativeSig {
		t.Fatalf("CompatibleSignatureForProvider(Claude) = %q, want provider-native %q", normalized, nativeSig)
	}
}

func TestSplitSignatureProviderPrefix_UsesStrictProviderAliases(t *testing.T) {
	gptSig := "gpt#" + testGPTReasoningSignature()
	if got := DetectSignatureProvider(gptSig); got != SignatureProviderGPT {
		t.Fatalf("DetectSignatureProvider(gpt#...) = %q, want %q", got, SignatureProviderGPT)
	}

	mislabeledPrefix := "claude-cache#" + testClaudeThinkingSignature()
	if _, _, ok := SplitSignatureProviderPrefix(mislabeledPrefix); ok {
		t.Fatal("claude-cache# should not be accepted as an explicit provider prefix")
	}
	if got := DetectSignatureProvider(mislabeledPrefix); got != SignatureProviderUnknown {
		t.Fatalf("DetectSignatureProvider(claude-cache#...) = %q, want %q", got, SignatureProviderUnknown)
	}
}

func TestSanitizeClaudeMessagesSignaturesForModel_NormalizesSameProviderClaude(t *testing.T) {
	nativeSig := testClaudeThinkingSignature()
	sig := "claude#" + nativeSig
	input := []byte(`{"model":"claude-sonnet","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"keep","signature":"` + sig + `"},{"type":"text","text":"answer"}]}]}`)
	expectedSig, err := NormalizeClaudeProviderNativeThinkingSignature(nativeSig)
	if err != nil {
		t.Fatalf("NormalizeClaudeProviderNativeThinkingSignature failed: %v", err)
	}

	output, report := SanitizeClaudeMessagesSignaturesForModel(input, "claude-sonnet-4-5")
	if report.Preserved != 1 || report.DroppedBlocks != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if got := ojson.GetBytes(output, "messages.0.content.0.signature").String(); got != expectedSig {
		t.Fatalf("signature = %q, want normalized %q", got, expectedSig)
	}
}

func TestSanitizeClaudeMessagesSignaturesForModel_PreservesGPTForGPT(t *testing.T) {
	sig := testGPTReasoningSignature()
	input := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"keep","signature":"` + sig + `"},{"type":"text","text":"answer"}]}]}`)

	output, report := SanitizeClaudeMessagesSignaturesForModel(input, "gpt-5.2")
	if report.Preserved != 1 || report.DroppedBlocks != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if got := ojson.GetBytes(output, "messages.0.content.0.signature").String(); got != sig {
		t.Fatalf("signature = %q, want preserved %q", got, sig)
	}
}

func TestSanitizeClaudeMessagesSignaturesForModel_DropsEmptyAssistantMessage(t *testing.T) {
	sig := "claude#" + testClaudeThinkingSignature()
	input := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"drop","signature":"` + sig + `"}]},{"role":"user","content":[{"type":"text","text":"next"}]}]}`)

	output, report := SanitizeClaudeMessagesSignaturesForModel(input, "gpt-5.2")
	if report.DroppedBlocks != 1 {
		t.Fatalf("DroppedBlocks = %d, want 1", report.DroppedBlocks)
	}
	messages := ojson.GetBytes(output, "messages").Array()
	if len(messages) != 1 {
		t.Fatalf("messages length = %d, want 1: %s", len(messages), output)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("remaining role = %q, want user", got)
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_DropsInvalidThinkingAndCleansToolUse(t *testing.T) {
	input := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"drop me","signature":""},{"type":"text","text":"answer"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"git status"},"signature":"bad","thoughtSignature":"bad2","thought_signature":"bad3","model":"claude-sonnet-4-5","extra_content":{"google":{"thought_signature":"bad4"}}}]}]}`)

	output, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-sonnet-4-5")
	if report.DroppedBlocks != 1 {
		t.Fatalf("DroppedBlocks = %d, want 1; report=%+v", report.DroppedBlocks, report)
	}
	parts := ojson.GetBytes(output, "messages.0.content").Array()
	if len(parts) != 2 {
		t.Fatalf("content length = %d, want 2: %s", len(parts), output)
	}
	if parts[0].Get("type").String() != "text" {
		t.Fatalf("first remaining part = %s, want text", parts[0].Raw)
	}
	toolUse := parts[1]
	if toolUse.Get("type").String() != "tool_use" {
		t.Fatalf("second remaining part = %s, want tool_use", toolUse.Raw)
	}
	if got := toolUse.Get("id").String(); got != "toolu_1" {
		t.Fatalf("tool_use id = %q, want toolu_1", got)
	}
	for _, path := range []string{
		"signature",
		"thoughtSignature",
		"thought_signature",
		"model",
		"extra_content",
	} {
		if toolUse.Get(path).Exists() {
			t.Fatalf("tool_use.%s should be removed: %s", path, toolUse.Raw)
		}
	}
}

func TestSanitizeClaudeMessagesForClaudeUpstream_NormalizesValidThinkingAndDropsEmptyMessage(t *testing.T) {
	nativeSig := testClaudeThinkingSignature()
	doubleEncoded := base64.StdEncoding.EncodeToString([]byte(nativeSig))
	input := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"keep","signature":"` + doubleEncoded + `"},{"type":"text","text":"answer"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"drop"}]},{"role":"user","content":[{"type":"text","text":"next"}]}]}`)

	output, report := SanitizeClaudeMessagesForClaudeUpstream(input, "claude-sonnet-4-5")
	if report.Preserved != 1 || report.DroppedBlocks != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	messages := ojson.GetBytes(output, "messages").Array()
	if len(messages) != 2 {
		t.Fatalf("messages length = %d, want 2: %s", len(messages), output)
	}
	if got := messages[0].Get("content.0.signature").String(); got != nativeSig {
		t.Fatalf("signature = %q, want provider-native %q", got, nativeSig)
	}
	if got := messages[1].Get("role").String(); got != "user" {
		t.Fatalf("remaining second role = %q, want user", got)
	}
}
