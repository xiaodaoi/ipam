// licensegen 离线 License 签发工具（M6-004）。
//
// 生成密钥对：
//
//	licensegen -gen-key
//
// 签发：
//
//	IPAM_LICENSE_PRIVATE_KEY=<base64> licensegen -id LIC-1 -to ACME -edition enterprise \
//	  -features ha,ldap -max 5000 -days 365 -out license.json
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type payload struct {
	LicenseID    string     `json:"licenseId"`
	IssuedTo     string     `json:"issuedTo"`
	Edition      string     `json:"edition"`
	Features     []string   `json:"features"`
	MaxTerminals int        `json:"maxTerminals"`
	IssuedAt     time.Time  `json:"issuedAt"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	GraceDays    int        `json:"graceDays,omitempty"`
}

func main() {
	var (
		genKey   = flag.Bool("gen-key", false, "生成 Ed25519 密钥对（stdout：private/public base64）")
		id       = flag.String("id", "", "License ID")
		to       = flag.String("to", "", "授权客户/项目")
		edition  = flag.String("edition", "standard", "lite|standard|enterprise")
		features = flag.String("features", "", "逗号分隔能力，如 ha,ldap,siem,oem")
		maxTerm  = flag.Int("max", 0, "终端上限（0=不限）")
		days     = flag.Int("days", 365, "有效天数（0=永久）")
		out      = flag.String("out", "", "输出文件（缺省 stdout）")
		privB64  = flag.String("private-key", os.Getenv("IPAM_LICENSE_PRIVATE_KEY"), "Ed25519 private key base64")
	)
	flag.Parse()

	if *genKey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("IPAM_LICENSE_PRIVATE_KEY=%s\n", base64.StdEncoding.EncodeToString(priv))
		fmt.Printf("IPAM_LICENSE_PUBKEY=%s\n", base64.StdEncoding.EncodeToString(pub))
		return
	}
	if *privB64 == "" {
		fatal(fmt.Errorf("缺少 private key（-private-key 或 IPAM_LICENSE_PRIVATE_KEY）"))
	}
	rawKey, err := base64.StdEncoding.DecodeString(*privB64)
	if err != nil {
		fatal(fmt.Errorf("private key base64: %w", err))
	}
	if len(rawKey) != ed25519.PrivateKeySize {
		fatal(fmt.Errorf("private key 长度错误: %d", len(rawKey)))
	}
	priv := ed25519.PrivateKey(rawKey)

	if *id == "" || *to == "" {
		fatal(fmt.Errorf("-id 与 -to 必填"))
	}
	if *edition != "lite" && *edition != "standard" && *edition != "enterprise" {
		fatal(fmt.Errorf("edition 非法"))
	}
	var fs []string
	if strings.TrimSpace(*features) != "" {
		for _, f := range strings.Split(*features, ",") {
			if f = strings.TrimSpace(f); f != "" {
				fs = append(fs, f)
			}
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	p := payload{
		LicenseID: *id, IssuedTo: *to, Edition: *edition, Features: fs,
		MaxTerminals: *maxTerm, IssuedAt: now,
	}
	if *days > 0 {
		exp := now.Add(time.Duration(*days) * 24 * time.Hour)
		p.ExpiresAt = &exp
	}
	raw, err := json.Marshal(p)
	if err != nil {
		fatal(err)
	}
	env := envelope{
		Payload:   base64.RawURLEncoding.EncodeToString(raw),
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, raw)),
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		fatal(err)
	}
	b = append(b, '\n')
	if *out == "" {
		_, _ = os.Stdout.Write(b)
		return
	}
	if err := os.WriteFile(*out, b, 0o600); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "license written: %s\n", *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "licensegen:", err)
	os.Exit(1)
}
