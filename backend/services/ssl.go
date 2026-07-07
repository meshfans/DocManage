package services

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"doc/utils"
)

type SSLCertInfo struct {
	PublicCertPath string `json:"public_cert_path"`
	PrivateKeyPath string `json:"private_key_path"`
	CommonName     string `json:"common_name"`
	ExpiredDays    int    `json:"expired_days"`
	GeneratedAt    string `json:"generated_at"`
}

func GenerateSelfSignedCert(certDir, commonName string, expiredDays int) (*SSLCertInfo, error) {
	if expiredDays <= 0 {
		expiredDays = 365
	}

	if certDir == "" {
		certDir = "./certs"
	}

	if commonName == "" {
		commonName = "localhost"
	}

	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, err
	}

	utils.Info("[SSL] 开始生成自签名证书...")
	utils.Info("[SSL] 证书目录: %s", certDir)
	utils.Info("[SSL] 通用名称: %s", commonName)
	utils.Info("[SSL] 有效期: %d天", expiredDays)

	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, expiredDays),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost", commonName},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	certPath := filepath.Join(certDir, "public.pem")
	keyPath := filepath.Join(certDir, "private.pem")

	certPath = strings.ReplaceAll(certPath, "\\", "/")
	keyPath = strings.ReplaceAll(keyPath, "\\", "/")

	certOut, err := os.Create(certPath)
	if err != nil {
		return nil, err
	}
	defer certOut.Close()

	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	utils.Info("[SSL] 证书已保存: %s", certPath)

	keyOut, err := os.Create(keyPath)
	if err != nil {
		return nil, err
	}
	defer keyOut.Close()

	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	utils.Info("[SSL] 私钥已保存: %s", keyPath)

	if err := os.Chmod(keyPath, 0600); err != nil {
		utils.Warn("[SSL] 警告: 设置私钥权限失败: %v", err)
	}

	utils.Info("[SSL] ✅ 自签名证书生成成功！")

	return &SSLCertInfo{
		PublicCertPath: certPath,
		PrivateKeyPath: keyPath,
		CommonName:     commonName,
		ExpiredDays:    expiredDays,
		GeneratedAt:    time.Now().Format(time.RFC3339),
	}, nil
}
