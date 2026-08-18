// storage/sigv4_vectors_test.go — KNOWN-ANSWER vectors for the one HMAC request
// signer inbrix actually ships (AWS SigV4 over the optional object-storage
// seam). The published, implementable spec these pin is docs/SIGNING.md.
//
// WHY A GOLDEN TEST AND NOT A SHAPE TEST: TestSigV4Shape (object_test.go) proves
// the Authorization header LOOKS right — right prefix, right SignedHeaders list,
// 64 lowercase hex. That passes even if the canonical preimage silently changes
// (a reordered header, a dropped empty query line, a different URI encoding),
// which would break interoperability with every real S3 server while every
// existing test stayed green. These vectors pin the exact bytes.
//
// The expected signatures below were derived from the PROSE in docs/SIGNING.md by
// a separate implementation, not copied out of object.go. So if sign() and the
// document ever disagree, this test fails — which is the point: the document is
// the contract, and a second implementer working only from it must land on these
// same bytes.
package storage

import (
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sigV4Vector is one published vector. Keep these fields, the expectations, and
// the "Vectors" table in docs/SIGNING.md in lockstep.
type sigV4Vector struct {
	name string

	// Request + store configuration.
	method    string
	host      string
	bucket    string
	prefix    string // gateway prefix + "mail/", exactly as joinPrefix produces
	key       string
	region    string
	accessKey string
	secretKey string
	sessToken string
	payload   []byte
	when      time.Time

	// Expected wire values.
	canonicalURI  string
	payloadHash   string
	signedHeaders string
	signature     string
}

// sigV4Vectors is the published corpus. Every vector here MUST also appear in
// docs/SIGNING.md; TestSigV4VectorCoverage asserts the count so a vector cannot
// be quietly dropped and leave the suite passing by doing less work.
var sigV4Vectors = []sigV4Vector{
	{
		name:      "GET/empty-payload/no-session-token",
		method:    http.MethodGet,
		host:      "s3.example.com",
		bucket:    "inbrix-test",
		prefix:    "tenant-a/mail/",
		key:       "attachments/INBOX/42/2.1",
		region:    "eu-west-2",
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		payload:   nil,
		when:      time.Date(2026, 7, 28, 12, 34, 56, 0, time.UTC),

		canonicalURI:  "/inbrix-test/tenant-a/mail/attachments/INBOX/42/2.1",
		payloadHash:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		signedHeaders: "host;x-amz-content-sha256;x-amz-date",
		signature:     "1a277eeb8fddd19cc0201046366b38cfbaae3f22a936e2bc77c2b72f7fad8463",
	},
	{
		// Exercises the three things vector A cannot: a non-empty payload hash, the
		// conditional x-amz-security-token header (which changes SignedHeaders), and
		// a key whose bytes must be percent-encoded in the canonical URI.
		name:      "PUT/body/session-token/percent-encoded-key",
		method:    http.MethodPut,
		host:      "s3.example.com",
		bucket:    "inbrix-test",
		prefix:    "tenant-a/mail/",
		key:       "attachments/INBOX/42/invoice #1.pdf",
		region:    "eu-west-2",
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		sessToken: "FQoGZXIvYXdzEXAMPLETOKEN",
		payload:   []byte("inbrix"),
		when:      time.Date(2026, 7, 28, 12, 34, 56, 0, time.UTC),

		canonicalURI:  "/inbrix-test/tenant-a/mail/attachments/INBOX/42/invoice%20%231.pdf",
		payloadHash:   "c54a21d1cfab8a341d973602e7160654c67a494cd0c17ed14062ee9fd042c19e",
		signedHeaders: "host;x-amz-content-sha256;x-amz-date;x-amz-security-token",
		signature:     "49ce5859384d80d60e659ead8404d8b0f8ee424f028143e16031405aee42c308",
	},
}

// TestSigV4KnownAnswerVectors runs the real signer over every published vector
// and asserts the exact canonical URI, payload hash, SignedHeaders list, credential
// scope, and signature.
func TestSigV4KnownAnswerVectors(t *testing.T) {
	for _, v := range sigV4Vectors {
		t.Run(v.name, func(t *testing.T) {
			s := &s3Store{
				scheme:    "https",
				host:      v.host,
				bucket:    v.bucket,
				prefix:    v.prefix,
				region:    v.region,
				accessKey: v.accessKey,
				secretKey: v.secretKey,
				sessToken: v.sessToken,
			}

			u, canonURI := s.buildURL(v.key)
			if canonURI != v.canonicalURI {
				t.Fatalf("canonical URI\n got: %s\nwant: %s", canonURI, v.canonicalURI)
			}

			payloadHash := hashHex(v.payload)
			if payloadHash != v.payloadHash {
				t.Fatalf("payload hash\n got: %s\nwant: %s", payloadHash, v.payloadHash)
			}

			req, err := http.NewRequest(v.method, u.String(), nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.URL = u
			s.sign(req, canonURI, payloadHash, v.when)

			// The signed timestamp must be the one we passed, not wall-clock: a signer
			// that ignored `now` would still produce a valid-looking header.
			if got, want := req.Header.Get("X-Amz-Date"), v.when.Format("20060102T150405Z"); got != want {
				t.Fatalf("X-Amz-Date = %q, want %q", got, want)
			}
			if got := req.Header.Get("X-Amz-Content-Sha256"); got != v.payloadHash {
				t.Fatalf("X-Amz-Content-Sha256 = %q, want %q", got, v.payloadHash)
			}
			if got, want := req.Header.Get("X-Amz-Security-Token"), v.sessToken; got != want {
				t.Fatalf("X-Amz-Security-Token = %q, want %q", got, want)
			}

			auth := req.Header.Get("Authorization")
			scope := v.when.Format("20060102") + "/" + v.region + "/s3/aws4_request"
			wantAuth := "AWS4-HMAC-SHA256 Credential=" + v.accessKey + "/" + scope +
				", SignedHeaders=" + v.signedHeaders +
				", Signature=" + v.signature
			if auth != wantAuth {
				t.Fatalf("Authorization mismatch\n got: %s\nwant: %s", auth, wantAuth)
			}
		})
	}
}

// TestSigV4SignatureIsKeyed guards the failure mode a golden vector alone cannot
// catch: a signer that produced a stable-but-unkeyed digest would match its own
// pinned value forever. Changing ONLY the secret key, ONLY the region, ONLY the
// timestamp, or ONLY the payload must each change the signature.
func TestSigV4SignatureIsKeyed(t *testing.T) {
	base := sigV4Vectors[0]

	sign := func(mut func(*sigV4Vector)) string {
		v := base
		mut(&v)
		s := &s3Store{
			scheme: "https", host: v.host, bucket: v.bucket, prefix: v.prefix,
			region: v.region, accessKey: v.accessKey, secretKey: v.secretKey,
			sessToken: v.sessToken,
		}
		u, canonURI := s.buildURL(v.key)
		req, _ := http.NewRequest(v.method, u.String(), nil)
		req.URL = u
		s.sign(req, canonURI, hashHex(v.payload), v.when)
		auth := req.Header.Get("Authorization")
		return auth[strings.Index(auth, "Signature=")+len("Signature="):]
	}

	unchanged := sign(func(*sigV4Vector) {})
	if unchanged != base.signature {
		t.Fatalf("control run drifted from the vector: %s != %s", unchanged, base.signature)
	}

	for _, tc := range []struct {
		what string
		mut  func(*sigV4Vector)
	}{
		{"secret key", func(v *sigV4Vector) { v.secretKey += "x" }},
		{"region", func(v *sigV4Vector) { v.region = "us-east-1" }},
		{"timestamp", func(v *sigV4Vector) { v.when = v.when.Add(time.Second) }},
		{"payload", func(v *sigV4Vector) { v.payload = []byte("different") }},
		{"object key", func(v *sigV4Vector) { v.key += ".bak" }},
		{"bucket", func(v *sigV4Vector) { v.bucket = "other-bucket" }},
		{"method", func(v *sigV4Vector) { v.method = http.MethodPut }},
		{"session token", func(v *sigV4Vector) { v.sessToken = "TOKEN" }},
	} {
		if got := sign(tc.mut); got == unchanged {
			t.Errorf("changing the %s did not change the signature (%s) — the signer is not binding it", tc.what, got)
		}
	}
}

// TestSigV4VectorCoverage asserts the published corpus cannot silently shrink and
// that each vector is well-formed. A harness that "passes" over an empty or
// truncated corpus is worse than no harness.
func TestSigV4VectorCoverage(t *testing.T) {
	const wantVectors = 2 // keep in step with the Vectors table in docs/SIGNING.md
	if len(sigV4Vectors) < wantVectors {
		t.Fatalf("only %d SigV4 vectors defined, want at least %d — vectors were removed without updating docs/SIGNING.md",
			len(sigV4Vectors), wantVectors)
	}

	seenSigs := map[string]string{}
	sawSessionToken, sawNonEmptyPayload, sawEncodedKey := false, false, false
	for _, v := range sigV4Vectors {
		if len(v.signature) != 64 {
			t.Errorf("%s: signature is %d chars, want 64 hex", v.name, len(v.signature))
		}
		if _, err := hex.DecodeString(v.signature); err != nil {
			t.Errorf("%s: signature is not hex: %v", v.name, err)
		}
		if prev, dup := seenSigs[v.signature]; dup {
			t.Errorf("%s and %s share a signature — one of them is not exercising what it claims", v.name, prev)
		}
		seenSigs[v.signature] = v.name

		if v.sessToken != "" {
			sawSessionToken = true
		}
		if len(v.payload) > 0 {
			sawNonEmptyPayload = true
		}
		if strings.Contains(v.canonicalURI, "%") {
			sawEncodedKey = true
		}
	}
	if !sawSessionToken {
		t.Error("no vector carries a session token — the conditional x-amz-security-token branch is unverified")
	}
	if !sawNonEmptyPayload {
		t.Error("no vector carries a non-empty payload — the payload-hash binding is unverified")
	}
	if !sawEncodedKey {
		t.Error("no vector needs percent-encoding — awsURIEncode's role in the canonical URI is unverified")
	}
}

// TestNoOutboundWebhookSigner is a standing assertion, not a formality. inbrix
// deliberately ships NO outbound webhook emitter and NO bespoke webhook HMAC — the
// suite already carries several mutually incompatible ones, and docs/SIGNING.md
// records inbrix's position that it has none. If someone adds one, this test
// fails and forces the wire format to be specified + vectored in docs/SIGNING.md
// before it can ship, rather than becoming one more undocumented dialect.
//
// No count of the suite's webhook dialects is asserted here on purpose. inbrix
// has no webhook signing at all, so it is not a member of that set, and this
// repo cannot verify the other repos' designs — a number stated here would only
// feed a suite-wide tally that wrongly includes inbrix.
//
// It works by construction: the ONLY hmac.New call site permitted in the repo is
// hmacSHA256 in this package (AWS SigV4). The check is a compile-time-adjacent
// invariant expressed as a test so it runs in CI on every push.
func TestNoOutboundWebhookSigner(t *testing.T) {
	hits, err := grepRepo(`hmac\.New\(`)
	if err != nil {
		t.Skipf("SKIPPING outbound-webhook-signer check: could not scan the repo (%v). "+
			"NOT VERIFIED: that inbrix still ships exactly one HMAC signer (AWS SigV4) "+
			"and no bespoke webhook HMAC.", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hmac.New call sites found at all — the scan is broken, so this test would " +
			"pass by doing nothing; fix grepRepo rather than deleting this test")
	}
	for _, h := range hits {
		if h.file == "storage/object.go" {
			continue // AWS SigV4 — the one signer, specified in docs/SIGNING.md
		}
		t.Errorf("%s:%d introduces an HMAC signer outside the documented AWS SigV4 path (%s).\n"+
			"If this is an outbound webhook signature, its exact wire format (header names, "+
			"canonical preimage, timestamp window, nonce handling, retry semantics) must be "+
			"added to docs/SIGNING.md with known-answer vectors before it ships.", h.file, h.line, h.text)
	}
}
