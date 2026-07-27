// ============================================================================
// Robin Trading Platform — Desktop Notifications
// Provides native Windows toast notifications for high-priority events.
// ============================================================================

package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"unicode/utf16"
)

// Notify sends a desktop notification if supported on the host OS.
func Notify(title, message string) {
	if runtime.GOOS != "windows" {
		log.Printf("[NOTIFY] %s: %s", title, message)
		return
	}

	// On Windows, use PowerShell to trigger a native toast notification.
	// Title/message are base64-encoded to prevent PowerShell injection via $() or backticks.
	titleB64 := base64.StdEncoding.EncodeToString([]byte(title))
	msgB64 := base64.StdEncoding.EncodeToString([]byte(message))
	psScript := fmt.Sprintf(`
$title = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String("%s"))
$message = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String("%s"))
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$textNodes = $template.GetElementsByTagName("text")
$textNodes.Item(0).AppendChild($template.CreateTextNode($title)) > $null
$textNodes.Item(1).AppendChild($template.CreateTextNode($message)) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Robin Platform")
$notifier.Show($toast)
`, titleB64, msgB64)

	// Encode as UTF-16LE base64 for -EncodedCommand to avoid shell interpretation entirely
	utf16le := utf16.Encode([]rune(psScript))
	encodedBytes := make([]byte, len(utf16le)*2)
	for i, r := range utf16le {
		encodedBytes[i*2] = byte(r)
		encodedBytes[i*2+1] = byte(r >> 8)
	}
	encodedCmd := base64.StdEncoding.EncodeToString(encodedBytes)

	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-EncodedCommand", encodedCmd)
	if err := cmd.Run(); err != nil {
		log.Printf("[NOTIFY ERR] Failed to send toast: %v", err)
	}
}
