package douyin

import (
	"encoding/hex"
	"testing"
)

func TestDouyinSM3OfficialVector(t *testing.T) {
	digest := douyinSM3Sum([]byte("abc"))
	if got, want := hex.EncodeToString(digest[:]), "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"; got != want {
		t.Fatalf("SM3(abc) = %s, want %s", got, want)
	}
}

func TestDouyinABogusMatchesReferenceVector(t *testing.T) {
	const query = "device_platform=webapp&aid=6383&aweme_id=7679356419690253583"
	const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"
	const want = "E7mhBdugDifihdWk56KLfY3q6AM3Y0xI0trEMD2fFn37qL39HMTa9exoIBGvXFEjwG/-IeYjy4hbT3ohrQ2y8qwf9W0L/25gsDSkKl12so0j53inCLf/E0iE5hsAtFH8svr4iKi8owICSYyhldAJ5kIlO62-zo0/9vY="
	got := douyinABogusWithInputs(query, userAgent, 1700000000123, 1700000000123, [3]int{1234, 5678, 9012})
	if got != want {
		t.Fatalf("a_bogus reference mismatch\n got: %s\nwant: %s", got, want)
	}
}
