package notify

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"

    "alert_framework/internal/config"
)

// Message represents outbound alert.
type Message struct {
    Text string `json:"text"`
}

// SendGroupMe posts alert to GroupMe bot if configured.
func SendGroupMe(cfg config.Config, msg Message) error {
    if cfg.GroupMeBotID == "" {
        return nil
    }
    payload := map[string]string{"text": msg.Text, "bot_id": cfg.GroupMeBotID}
    buf, _ := json.Marshal(payload)
    req, _ := http.NewRequest(http.MethodPost, cfg.GroupMeURL, bytes.NewBuffer(buf))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 300 {
        return fmt.Errorf("groupme status %d", resp.StatusCode)
    }
    return nil
}
