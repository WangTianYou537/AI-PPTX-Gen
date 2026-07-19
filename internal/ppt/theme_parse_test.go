package ppt

import "testing"

func TestParseVisualDesignLockJSON(t *testing.T) {
	raw := "```json\n{\"mode\":\"mono\",\"background\":\"#FFFFFF\",\"surface\":\"#FFFFFF\",\"primaryText\":\"#111111\",\"secondaryText\":\"#333333\",\"accent\":\"#111111\",\"accentAlt\":\"#555555\",\"border\":\"#111111\",\"notes\":[\"黑白线条\"]}\n```"
	lock, err := ParseVisualDesignLockJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Mode != "mono" || lock.Background != "#FFFFFF" || lock.PrimaryText != "#111111" {
		t.Fatalf("%+v", lock)
	}
}
