// ============================================================================
// Robin Trading Platform — Desktop Notifications
// Provides native Windows toast notifications for high-priority events.
// ============================================================================

package main

import (
	"log"
	"os/exec"
	"strings"
	"runtime"
)

// Notify sends a desktop notification if supported on the host OS.
func Notify(title, message string) {
	if runtime.GOOS != "windows" {
		log.Printf("[NOTIFY] %s: %s", title, message)
		return
	}

	// On Windows, use PowerShell to trigger a native toast notification
	// This avoids needing external CGO libraries for notifications.
	psScript := `
	[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
	$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
	$textNodes = $template.GetElementsByTagName("text")
	$textNodes.Item(0).AppendChild($template.CreateTextNode("` + escapePS(title) + `")) > $null
	$textNodes.Item(1).AppendChild($template.CreateTextNode("` + escapePS(message) + `")) > $null
	$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
	$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Robin Platform")
	$notifier.Show($toast)
	`
	
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", psScript)
	if err := cmd.Run(); err != nil {
		log.Printf("[NOTIFY ERR] Failed to send toast: %v", err)
	}
}

func escapePS(s string) string {
	s = strings.ReplaceAll(s, "`", "``")
	s = strings.ReplaceAll(s, "\"", "`\"")
	return s
}
