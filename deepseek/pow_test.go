package deepseek

import (
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

// Vectors captured from the live web client (docs/api-analysis.md):
// the web client produced exactly these (salt, expire_at, answer) triples
// and the server accepted them.
func TestDeepSeekHashV1Vectors(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"a40318e6b7af56bf27cf_1790959021012_58595", "168efa309e5a73033fc82dde4333bbc6515da440ef961574f73f99cbe7b397ad"},
		{"0b846826f9b7ab6000f6_1790959592240_44460", "ab9178c48249ac301272e3e24b9a5aef207e593add3003bfd7a8fb8abb572079"},
	}
	for i, c := range cases {
		got := hex.EncodeToString(toBytes(deepSeekHashV1([]byte(c.msg))))
		if got != c.want {
			t.Fatalf("vector %d: got %s want %s", i, got, c.want)
		}
	}
}

func TestSolvePow(t *testing.T) {
	c := PowChallenge{
		Challenge:  "168efa309e5a73033fc82dde4333bbc6515da440ef961574f73f99cbe7b397ad",
		Salt:       "a40318e6b7af56bf27cf",
		ExpireAt:   1790959021012,
		Difficulty: 144000,
	}
	start := time.Now()
	nonce, err := c.Solve()
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	t.Logf("solved nonce=%d in %s", nonce, time.Since(start))
	msg := c.Salt + "_" + itoa(c.ExpireAt) + "_" + itoa(int64(nonce))
	got := hex.EncodeToString(toBytes(deepSeekHashV1([]byte(msg))))
	if got != c.Challenge {
		t.Fatalf("solved nonce does not verify: %s", got)
	}
}

func toBytes(h [32]byte) []byte { return h[:] }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
