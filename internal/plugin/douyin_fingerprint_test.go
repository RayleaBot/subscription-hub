package plugin

import (
	"strings"
	"testing"
)

func TestDouyinBrowserFingerprintShape(t *testing.T) {
	fp := douyinBrowserFingerprint()
	if !strings.HasSuffix(fp, "|24|24|Win32") {
		t.Fatalf("fingerprint = %q", fp)
	}
	if len(strings.Split(fp, "|")) != 17 {
		t.Fatalf("fingerprint fields = %q", fp)
	}
}
