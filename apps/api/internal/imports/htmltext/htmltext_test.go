package htmltext

import (
	"strings"
	"testing"
)

func TestFromHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		opts Options
		want string
	}{
		{"paragraphs", "<p>First   paragraph\nwraps.</p><p>Second.</p>", Options{}, "First paragraph wraps.\n\nSecond."},
		{"inline formatting", "<p>Use <strong>queue</strong> numbers, <em>not</em> names and <code>ticket_id</code>.</p>", Options{},
			"Use **queue** numbers, *not* names and `ticket_id`."},
		{"space inside inline", "<p>a<b> bold </b>word</p>", Options{}, "a **bold** word"},
		{"heading", "<h2>Privacy</h2><p>Use numbers.</p>", Options{}, "## Privacy\n\nUse numbers."},
		{"unordered list", "<p>Show:</p><ul><li>Queue</li><li>Wait <b>time</b></li></ul><p>Done</p>", Options{},
			"Show:\n\n- Queue\n- Wait **time**\n\nDone"},
		{"ordered list with start", "<ol start=\"3\"><li>three</li><li>four</li></ol>", Options{}, "3. three\n4. four"},
		{"nested list", "<ul><li>Trips<ul><li>Create</li><li>Invite</li></ul></li><li>Chat</li></ul>", Options{},
			"- Trips\n  - Create\n  - Invite\n- Chat"},
		{"list item paragraphs", "<ul><li><p>One</p><p>More</p></li></ul>", Options{}, "- One\n\n  More"},
		{"code block", "<pre><code class=\"language-python\">def f():\n    return 1\n</code></pre>", Options{},
			"```python\ndef f():\n    return 1\n```"},
		{"link", "<p>See <a href=\"https://example.com/x\">the guide</a> or <a href=\"/rel\">here</a>.</p>", Options{},
			"See [the guide](https://example.com/x) or here."},
		{"blockquote", "<blockquote><p>Ride safe.</p><p>Always.</p></blockquote>", Options{}, "> Ride safe.\n>\n> Always."},
		{"table", "<table><tr><th>Plan</th><th>Price</th></tr><tr><td>Crew</td><td>$6</td></tr></table>", Options{},
			"| Plan | Price |\n| --- | --- |\n| Crew | $6 |"},
		{"br", "line one<br>line two", Options{}, "line one\nline two"},
		{"entities", "<p>Fish &amp; chips &lt;3</p>", Options{}, "Fish & chips <3"},
		{"drops scripts and styles", "<p>keep</p><script>alert(1)</script><style>p{}</style><noscript>no</noscript>", Options{}, "keep"},
		{"hidden elements", "<p>keep</p><div hidden>secret</div><span aria-hidden=\"true\">icon</span><p style=\"display: none\">x</p>", Options{}, "keep"},
		{"chrome kept by default", "<nav>Menu</nav><p>Body</p>", Options{}, "Menu\n\nBody"},
		{"chrome skipped", "<header>Site</header><nav>Menu</nav><p>Body</p><aside>Ads</aside><footer>(c)</footer>", Options{SkipChrome: true}, "Body"},
		{"image alt", "<p><img src=\"x.png\" alt=\"Trip card\"> <img src=\"y.png\"></p>", Options{}, "[image: Trip card]"},
		{"empty", "", Options{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromHTML(tt.html, tt.opts); got != tt.want {
				t.Errorf("FromHTML(%q)\n got: %q\nwant: %q", tt.html, got, tt.want)
			}
		})
	}
}

func TestFromHTMLDeepNestingDoesNotPanic(t *testing.T) {
	src := strings.Repeat("<div>", 5000) + "deep" + strings.Repeat("</div>", 5000)
	_ = FromHTML(src, Options{})
	src = strings.Repeat("<ul><li>", 2000) + "x"
	_ = FromHTML(src, Options{})
}
