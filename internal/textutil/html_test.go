package textutil

import "testing"

func TestHTMLToText(t *testing.T) {
	cases := map[string]string{
		"": "",
		"<p>Hello&nbsp;<b>world</b> &amp; co</p>":          "Hello world & co",
		"<div>line1</div><div>line2<br/>line3</div>":       "line1\nline2\nline3",
		"<ul><li>a</li><li>b</li></ul>":                    "- a\n- b",
		"<p>x</p><style>p{}</style><p></p><p></p><p>y</p>": "x\n\ny",
		`<at id="0">Jane Doe</at> please check`:            "Jane Doe please check",
	}
	for in, want := range cases {
		if got := HTMLToText(in); got != want {
			t.Errorf("HTMLToText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := BodyToText("text", "  <not html>  "); got != "<not html>" {
		t.Errorf("BodyToText text = %q", got)
	}
}
