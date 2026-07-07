package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"doc/utils"
)

//go:embed dist
var Dist embed.FS

func GetFS() (fs.FS, error) {
	subFS, err := fs.Sub(Dist, "dist")
	if err != nil {
		return nil, err
	}
	return subFS, nil
}

func ReadFile(path string) ([]byte, error) {
	fullPath := "dist/" + path
	return Dist.ReadFile(fullPath)
}

func FileExists(path string) bool {
	fullPath := "dist/" + path
	_, err := Dist.ReadFile(fullPath)
	return err == nil
}

func GetPlatformConfigWithDynamicUrl(serverHost, serverPort, domain string, enableSSL bool) ([]byte, error) {
	utils.Info("[GetPlatformConfig] Called with serverHost=%s, port=%s, domain=%s, enableSSL=%v", serverHost, serverPort, domain, enableSSL)

	content, err := ReadFile("platform-config.json")
	if err != nil {
		utils.Info("[GetPlatformConfig] Failed to read file: %v", err)
		return nil, err
	}

	address := serverHost
	if address == "0.0.0.0" {
		address = "localhost"
		utils.Info("[GetPlatformConfig] ServerHost was 0.0.0.0, changed to localhost")
	}

	if domain != "" {
		address = domain
		utils.Info("[GetPlatformConfig] Using domain as address: %s", domain)
	}

	newAddress := fmt.Sprintf("%s:%s", address, serverPort)
	utils.Info("[GetPlatformConfig] New address: %s", newAddress)

	var config map[string]interface{}
	if err := json.Unmarshal(content, &config); err != nil {
		utils.Info("[GetPlatformConfig] Failed to parse JSON: %v", err)
		return nil, err
	}

	updateUrl := func(key string) {
		if url, ok := config[key].(string); ok && url != "" {
			protocol := getProtocol(url)
			path := getPath(url)

			utils.Info("[GetPlatformConfig] Original %s: %s", key, url)
			utils.Info("[GetPlatformConfig] Protocol: %s, Path: %s", protocol, path)

			if enableSSL {
				if strings.HasPrefix(protocol, "http:") {
					protocol = "https://"
					utils.Info("[GetPlatformConfig] Changed %s protocol to HTTPS", key)
				} else if strings.HasPrefix(protocol, "ws:") {
					protocol = "wss://"
					utils.Info("[GetPlatformConfig] Changed %s protocol to WSS", key)
				}
			} else {
				utils.Info("[GetPlatformConfig] SSL disabled, keeping %s protocol", protocol)
			}

			parsedUrl := protocol + newAddress + path
			config[key] = parsedUrl
			utils.Info("[GetPlatformConfig] Updated %s: %s", key, parsedUrl)
		} else {
			utils.Info("[GetPlatformConfig] Skipped %s (not found or empty)", key)
		}
	}

	utils.Info("[GetPlatformConfig] Updating URLs...")
	updateUrl("ApiBaseUrl")
	updateUrl("UploadUrl")
	updateUrl("WSUrl")

	result, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		utils.Info("[GetPlatformConfig] Failed to marshal JSON: %v", err)
		return nil, err
	}

	utils.Info("[GetPlatformConfig] Success, returning %d bytes", len(result))
	return result, nil
}

func getProtocol(url string) string {
	parts := strings.SplitN(url, "://", 2)
	if len(parts) < 2 {
		return "http://"
	}
	return parts[0] + "://"
}

func getPath(url string) string {
	parts := strings.SplitN(url, "://", 2)
	if len(parts) < 2 {
		return ""
	}
	pathParts := strings.SplitN(parts[1], "/", 2)
	if len(pathParts) < 2 {
		return ""
	}
	return "/" + pathParts[1]
}
