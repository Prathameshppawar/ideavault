package service

import "testing"

func TestPlaceholderTitle(t *testing.T) {
	for _, s := range []string{"New Idea from shared conversation", "New idea", "Untitled", "Imported conversation", "new idea from the shared link", "Shared conversation", "A new idea"} {
		if !placeholderTitle(s) {
			t.Errorf("%q should be treated as a placeholder", s)
		}
	}
	for _, s := range []string{"Build Zoho SharePoint Extension", "New idea board for bikers", "Untitled Goose clone", "Conversation analytics"} {
		if placeholderTitle(s) {
			t.Errorf("%q is a real title", s)
		}
	}
}
