package netease_music

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

const (
	neteaseNonce   = "0CoJUm6Qyw8W8jud"
	neteaseIV      = "0102030405060708"
	neteasePubKey  = "010001"
	neteaseModulus = "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7"
)

func neteaseRandomSecret() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i, b := range bytes {
		bytes[i] = chars[int(b)%len(chars)]
	}
	return string(bytes), nil
}

func neteaseWEAPIForm(payload string) (url.Values, error) {
	secret, err := neteaseRandomSecret()
	if err != nil {
		return nil, err
	}
	first, err := aesCBCBase64([]byte(payload), []byte(neteaseNonce), []byte(neteaseIV))
	if err != nil {
		return nil, err
	}
	params, err := aesCBCBase64([]byte(first), []byte(secret), []byte(neteaseIV))
	if err != nil {
		return nil, err
	}
	return url.Values{
		"params":    {params},
		"encSecKey": {neteaseEncSecKey(secret)},
	}, nil
}

func aesCBCBase64(plain, key, iv []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	padded, err := pkcs7Pad(plain, block.BlockSize())
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func pkcs7Pad(value []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 || blockSize > 255 {
		return nil, fmt.Errorf("invalid block size %d", blockSize)
	}
	padding := blockSize - len(value)%blockSize
	maxInt := int(^uint(0) >> 1)
	if len(value) > maxInt-padding {
		return nil, fmt.Errorf("payload too large for pkcs7 padding")
	}
	result := make([]byte, len(value)+padding)
	copy(result, value)
	for i := len(value); i < len(result); i++ {
		result[i] = byte(padding)
	}
	return result, nil
}

func neteaseEncSecKey(secret string) string {
	reversed := reverseASCII(secret)
	base := new(big.Int).SetBytes([]byte(reversed))
	exponent := new(big.Int)
	exponent.SetString(neteasePubKey, 16)
	modulus := new(big.Int)
	modulus.SetString(neteaseModulus, 16)
	encrypted := new(big.Int).Exp(base, exponent, modulus)
	return strings.Repeat("0", 256-len(encrypted.Text(16))) + encrypted.Text(16)
}

func reverseASCII(value string) string {
	bytes := []byte(value)
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
	return string(bytes)
}
