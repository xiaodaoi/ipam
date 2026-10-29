package platform

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// bootstrapPassword 返回首次启动的 admin 口令。
//
// 安全默认（M6-001）：
//   - 显式设置 IPAM_POC_PASSWORD 时沿用（兼容既有部署/自动化）；
//   - 未设置时生成一次性随机口令，只在启动日志与 /run/ipam/initial-admin-password
//     中展示一次；是否强制首次登录修改由系统设置「账号安全」决定（默认关闭）。
func bootstrapPassword() (password string, generated bool) {
	if p := strings.TrimSpace(os.Getenv("IPAM_POC_PASSWORD")); p != "" {
		return p, false
	}
	return randomPassword(24), true
}

// randomPassword 生成满足口令策略的随机口令（大小写/数字/符号各至少一类）。
func randomPassword(n int) string {
	if n < 16 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 熵源不可用即不能安全启动；与 JWT 密钥同策略。
		panic("ipam: crypto/rand 不可用，无法生成初始口令: " + err.Error())
	}
	core := base64.RawURLEncoding.EncodeToString(buf)
	if len(core) < n-7 {
		core = (core + core + core)[:n-7]
	}
	// 固定前后缀保证四类字符齐全：大写 I、小写 pam、数字 9、符号 !。
	return "Ipam-" + core[:n-7] + "!9"
}

// logBootstrapPassword 打印一次性初始口令并尽力写入运行目录（0600）。
// 密钥/口令不写数据库，只写本地一次性文件，避免入库与备份泄露。
func logBootstrapPassword(pw string, generated bool) {
	const marker = "==================== IPAM INITIAL ADMIN ===================="
	if generated {
		log.Printf("%s\n账号: admin\n初始口令: %s\n该口令仅显示一次；默认不强制改密，可在系统设置中开启。\n%s", marker, pw, marker)
	} else {
		log.Printf("%s\n账号: admin\n初始口令: 来自 IPAM_POC_PASSWORD（是否强制改密由系统设置决定，默认关闭）\n%s", marker, marker)
	}
	dir := os.Getenv("IPAM_RUN_DIR")
	if dir == "" {
		dir = "/run/ipam"
	}
	path := filepath.Join(dir, "initial-admin-password")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	content := "admin\n" + pw + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return
	}
	log.Printf("初始口令已写入 %s（0600），首次登录后请删除该文件", path)
}
