package svchttp

import (
	"context"
	"io"
	"net/http"

	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

// maxTextScan bounds how much of a plain-text answer is read while looking for its end.
const maxTextScan = 32 << 20

// GetTextTail performs a GET on a plain-text answer (a job log) and returns its last keep bytes. The whole answer is
// read, up to maxTextScan, because the useful part of a log is its end; only the tail is ever held in memory.
func GetTextTail(ctx context.Context, client *http.Client, service, address string, header http.Header, keep int) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", i18n.Wrap(err, "%s could not be reached: %s", service, err)
	}
	for key, values := range header {
		request.Header[key] = values
	}
	request.Header.Set("Accept", "text/plain")
	response, err := client.Do(request)
	if err != nil {
		return "", i18n.Wrap(err, "%s could not be reached: %s", service, unwrapTransport(err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return "", &StatusError{Service: service, Status: response.StatusCode, message: i18n.NewMessage("%s answered %d", service, response.StatusCode)}
	}
	tail := make([]byte, 0, keep*2)
	chunk := make([]byte, 32<<10)
	var scanned int
	for scanned < maxTextScan {
		n, readErr := response.Body.Read(chunk)
		tail = append(tail, chunk[:n]...)
		scanned += n
		if len(tail) > keep*2 {
			tail = append(tail[:0], tail[len(tail)-keep:]...)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", i18n.Wrap(readErr, "%s could not be reached: %s", service, unwrapTransport(readErr))
		}
	}
	if len(tail) > keep {
		tail = tail[len(tail)-keep:]
	}
	return string(tail), nil
}
