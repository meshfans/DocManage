# 详细设计：Tauri Windows 代码签名

> **任务编号**：todo.md #15  
> **优先级**：P0（阻塞商业化）  
> **前置条件**：等 EV 代码签名证书到账

## 1. 背景

`tauri build` 出 MSI/EXE 时，Windows SmartScreen 会拦截未签名/自签名的安装包，提示"未知发布者"并要求用户手动"仍要运行"，严重影响首启体验与商业可信度。

## 2. 方案对比

| 方案 | 一次性成本 | 年费 | SmartScreen 立即信任 | 推荐度 |
|------|-----------|------|---------------------|----------|
| **A. EV 代码签名证书** | — | $300-500/年 | ✅ **立即信任** | ⭐⭐⭐⭐⭐ |
| **B. OV 代码签名证书** | — | $100-300/年 | ⚠️ 需积累信誉 | ⭐⭐⭐⭐ |
| **C. Azure Trusted Signing** | — | ~$10/月（按使用）| ✅ 立即信任 | ⭐⭐⭐⭐ |
| **D. 自签名 / 跳过签名** | 0 | 0 | ❌ 警告 | ⭐ |

## 3. 推荐方案：EV 代码签名证书

| CA | 中文支持 | USB Token | 国内售价 | 备注 |
|----|---------|-----------|---------|------|
| **DigiCert** | ✅ | SafeNet 5110 | ¥8000-12000/年 | 企业首选，最稳定 |
| **GlobalSign** | ✅ | SafeNet 5110 | ¥6000-10000/年 | 性价比高 |
| **Sectigo** | ✅ | 自带 | ¥3000-6000/年 | 最便宜，签发快 |

## 4. Tauri 集成

**文件**：`src-tauri/tauri.conf.json`

```json
{
  "bundle": {
    "windows": {
      "certificateThumbprint": "A1B2C3D4E5F6...",
      "digestAlgorithm": "sha256",
      "timestampUrl": "http://timestamp.digicert.com"
    }
  }
}
```

## 5. 临时方案（仅限内部测试）

```powershell
# 生成自签名证书
$cert = New-SelfSignedCertificate -Subject "CN=DocManage Internal" `
  -Type CodeSigningCert -CertStoreLocation "Cert:\CurrentUser\My" `
  -NotAfter (Get-Date).AddYears(2)

# 导出 .pfx
$password = ConvertTo-SecureString -String "internal-test" -Force -AsPlainText
Export-PfxCertificate -Cert $cert -FilePath "C:\certs\internal.pfx" -Password $password

# 签名
signtool sign /f "C:\certs\internal.pfx" /p "internal-test" `
  "C:\path\to\docmanage.exe"
```

## 6. 相关资源

- Tauri 签名文档：https://v2.tauri.app/distribute/sign/windows/
- DigiCert EV 申请：https://www.digicert.com/signing/code-signing-certificates
- Azure Trusted Signing：https://learn.microsoft.com/azure/trusted-signing/
