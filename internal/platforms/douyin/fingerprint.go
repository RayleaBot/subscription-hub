package douyin

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// douyinBrowserFingerprint 生成 Win32 浏览器指纹（与参考实现同格式）。
func douyinBrowserFingerprint() string {
	innerWidth := 1024 + rand.IntN(897)
	innerHeight := 768 + rand.IntN(313)
	outerWidth := innerWidth + 24 + rand.IntN(9)
	outerHeight := innerHeight + 75 + rand.IntN(16)
	screenY := 0
	if rand.IntN(2) == 1 {
		screenY = 30
	}
	sizeWidth := 1024 + rand.IntN(897)
	sizeHeight := 768 + rand.IntN(313)
	availWidth := 1280 + rand.IntN(641)
	availHeight := 800 + rand.IntN(281)
	parts := []string{
		strconv.Itoa(innerWidth), strconv.Itoa(innerHeight), strconv.Itoa(outerWidth), strconv.Itoa(outerHeight),
		"0", strconv.Itoa(screenY), "0", "0", strconv.Itoa(sizeWidth), strconv.Itoa(sizeHeight),
		strconv.Itoa(availWidth), strconv.Itoa(availHeight), strconv.Itoa(innerWidth), strconv.Itoa(innerHeight), "24", "24", "Win32",
	}
	return strings.Join(parts, "|")
}

// douyinVerifyFp 生成 verifyFp / s_v_web_id（浏览器 JS 生成的设备标识，格式 verify_<base36时间戳>_<36位模式串>）。
// 与 f2 VerifyFpManager 同构：第 8/13/18/23 位为下划线，第 14 位为 '4'，第 19 位为 3&n|8。
func douyinVerifyFp() string {
	const base36Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// 毫秒时间戳转 base36。
	milliseconds := time.Now().UnixMilli()
	base36 := ""
	for milliseconds > 0 {
		remainder := milliseconds % 36
		if remainder < 10 {
			base36 = strconv.FormatInt(remainder, 10) + base36
		} else {
			base36 = string(rune('a'+remainder-10)) + base36
		}
		milliseconds /= 36
	}
	out := make([]byte, 36)
	for i := range out {
		n := rand.IntN(len(base36Chars))
		if i == 19 {
			n = 3&n | 8
		}
		out[i] = base36Chars[n]
	}
	out[8], out[13], out[18], out[23] = '_', '_', '_', '_'
	out[14] = '4'
	return "verify_" + base36 + "_" + string(out)
}
