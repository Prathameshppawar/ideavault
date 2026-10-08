package chatgptv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	chatgptv1 "github.com/Prathameshppawar/ideavault/apps/api/internal/imports/chatgpt/v1"
)

// maxDecodedBytes bounds the re-encoded share conversation.
const maxDecodedBytes = 256 << 20

// errNoConversation means the page does not embed a shared conversation.
var errNoConversation = errors.New("no shared conversation data found in page")

// sharePayload is what a share page yields: the conversation in ChatGPT
// export shape plus the public share id, when known.
type sharePayload struct {
	conv    chatgptv1.Conversation
	shareID string
}

// extractShare finds the shared conversation in a share page, trying the
// React Router turbo-stream format first and the older Next.js format second.
func extractShare(page string) (sharePayload, error) {
	if strings.Contains(page, enqueueCall) {
		p, err := extractTurboStream(page)
		if err == nil {
			return p, nil
		}
		if !strings.Contains(page, nextDataID) {
			return sharePayload{}, err
		}
	}
	if strings.Contains(page, nextDataID) {
		return extractNextData(page)
	}
	return sharePayload{}, errNoConversation
}

func extractTurboStream(page string) (sharePayload, error) {
	payloads := extractEnqueuePayloads(page)
	if len(payloads) == 0 {
		return sharePayload{}, errors.New("could not decode the embedded stream payload")
	}
	ts, err := parseTurboStream(strings.Join(payloads, ""))
	if err != nil {
		return sharePayload{}, err
	}
	data, shareID := findShareData(ts)
	if data == nil {
		return sharePayload{}, errNoConversation
	}
	raw, err := marshalBounded(data, maxDecodedBytes)
	if err != nil {
		return sharePayload{}, err
	}
	var conv chatgptv1.Conversation
	if err := json.Unmarshal(raw, &conv); err != nil {
		return sharePayload{}, fmt.Errorf("shared conversation: %w", err)
	}
	if shareID == "" {
		shareID = conv.ConversationID
	}
	return sharePayload{conv: conv, shareID: shareID}, nil
}

// findShareData locates serverResponse.data of the share route, falling back
// to any decoded object that carries a conversation.
func findShareData(ts *turboStream) (map[string]any, string) {
	if root, ok := ts.root().(map[string]any); ok {
		if loader, ok := root["loaderData"].(map[string]any); ok {
			keys := make([]string, 0, len(loader))
			for k := range loader {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if !strings.HasPrefix(k, "routes/share") {
					continue
				}
				route, _ := loader[k].(map[string]any)
				resp, _ := route["serverResponse"].(map[string]any)
				if data, ok := resp["data"].(map[string]any); ok && looksLikeConversation(data) {
					id, _ := route["sharedConversationId"].(string)
					return data, id
				}
			}
		}
	}
	for _, key := range []string{"linear_conversation", "mapping"} {
		if obj, ok := ts.findObjectWithKey(key); ok && looksLikeConversation(obj) {
			return obj, ""
		}
	}
	return nil, ""
}

func looksLikeConversation(obj map[string]any) bool {
	if lc, ok := obj["linear_conversation"].([]any); ok && len(lc) > 0 {
		return true
	}
	m, ok := obj["mapping"].(map[string]any)
	return ok && len(m) > 0
}

const nextDataID = `id="__NEXT_DATA__"`

// extractNextData handles the older Next.js share pages:
// <script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"serverResponse":{"data":{...}}}}}</script>
func extractNextData(page string) (sharePayload, error) {
	i := strings.Index(page, nextDataID)
	start := strings.IndexByte(page[i:], '>')
	if start < 0 {
		return sharePayload{}, errNoConversation
	}
	start += i + 1
	end := strings.Index(page[start:], "</script>")
	if end < 0 {
		return sharePayload{}, errNoConversation
	}
	var next struct {
		Props struct {
			PageProps struct {
				ServerResponse struct {
					Data json.RawMessage `json:"data"`
				} `json:"serverResponse"`
				SharedConversationID string `json:"sharedConversationId"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(page[start:start+end]), &next); err != nil {
		return sharePayload{}, fmt.Errorf("__NEXT_DATA__: %w", err)
	}
	if len(next.Props.PageProps.ServerResponse.Data) == 0 {
		return sharePayload{}, errNoConversation
	}
	var conv chatgptv1.Conversation
	if err := json.Unmarshal(next.Props.PageProps.ServerResponse.Data, &conv); err != nil {
		return sharePayload{}, fmt.Errorf("shared conversation: %w", err)
	}
	if len(conv.LinearConversation) == 0 && len(conv.Mapping) == 0 {
		return sharePayload{}, errNoConversation
	}
	id := next.Props.PageProps.SharedConversationID
	if id == "" {
		id = conv.ConversationID
	}
	return sharePayload{conv: conv, shareID: id}, nil
}
