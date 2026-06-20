package httpx

import (
    "fmt"
    "net/http"
    "time"
)

func DefaultClient() *http.Client {
    return &http.Client{Timeout: 30 * time.Second}
}

func CheckStatus(resp *http.Response) error {
    if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
        return nil
    }
    return fmt.Errorf("unexpected status: %s", resp.Status)
}
