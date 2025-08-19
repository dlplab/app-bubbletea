package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
)

type ProxmoxVM struct {
	VmID     int    `json:"vmid"`
	Name     string `json:"name"`
	Node     string `json:"node"`
	Template int    `json:"template"`
}

func getProxmoxCredsFromVault(cluster string) (apiUrl, tokenId, tokenSecret string, err error) {
	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		vaultAddr = "http://127.0.0.1:8200"
	}
	roleID := os.Getenv("TF_VAR_role_id")
	secretID := os.Getenv("TF_VAR_secret_id")
	if roleID == "" || secretID == "" {
		return "", "", "", fmt.Errorf("vault approle credentials not set")
	}

	cfg := vault.DefaultConfig()
	cfg.Address = vaultAddr
	client, err := vault.NewClient(cfg)
	if err != nil {
		return "", "", "", err
	}
	secret, err := client.Logical().Write("auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil || secret == nil || secret.Auth == nil {
		return "", "", "", fmt.Errorf("vault appRole login failed: %v", err)
	}
	client.SetToken(secret.Auth.ClientToken)

	secretPath := fmt.Sprintf("proxmox_api_keys/data/%s", cluster)
	kv, err := client.Logical().Read(secretPath)
	if err != nil || kv == nil || kv.Data == nil {
		return "", "", "", fmt.Errorf("vault read failed for %s: %v", secretPath, err)
	}
	data := kv.Data
	if v2, ok := data["data"].(map[string]interface{}); ok {
		data = v2
	}
	apiUrl, _ = data["proxmox_api_url"].(string)
	tokenId, _ = data["proxmox_api_token_id"].(string)
	tokenSecret, _ = data["proxmox_api_token_secret"].(string)
	if apiUrl == "" || tokenId == "" || tokenSecret == "" {
		return "", "", "", fmt.Errorf("missing fields in Vault secret %s", secretPath)
	}
	return apiUrl, tokenId, tokenSecret, nil
}

func listProxmoxTemplates(apiUrl, tokenId, tokenSecret string) ([]ProxmoxVM, error) {
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	url := fmt.Sprintf("https://%s:8006/api2/json/cluster/resources?type=vm", apiUrl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", tokenId, tokenSecret))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []ProxmoxVM `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var templates []ProxmoxVM
	for _, vm := range parsed.Data {
		if vm.Template == 1 {
			templates = append(templates, vm)
		}
	}
	return templates, nil
}

func fetchTemplatesForCluster(cluster string) ([]string, error) {
	apiURL, tokenID, tokenSecret, err := getProxmoxCredsFromVault(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get Proxmox creds from Vault: %w", err)
	}
	vms, err := listProxmoxTemplates(apiURL, tokenID, tokenSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to list Proxmox VMs: %w", err)
	}
	var templates []string
	includeRe := regexp.MustCompile(`^ubuntu-server-24\.04\..*`)
	for _, vm := range vms {
		if vm.Template == 1 {
			name := vm.Name
			if includeRe.MatchString(name) && !strings.HasSuffix(name, "-test") {
				templates = append(templates, name)
			}
		}
	}
	return templates, nil
}
