package observe

import "testing"

func TestMemberBytesMatchPythonGoldenJSON(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"null", "null\n"},
		{"true", "true\n"},
		{"{\"\u9e48\u9e55\": \"<>&\u2028\u2029\\n\\\"\\\\\", \"a\": 1.0}", "{\"a\":1.0,\"\u9e48\u9e55\":\"<>&\u2028\u2029\\n\\\"\\\\\"}\n"},
		{"[1e-07, 0.0001, 1000000000000000.0, 1e+16, -0.0, 1.2345678901234567, 1.7976931348623157e+308, 5e-324]", "[1e-07,0.0001,1000000000000000.0,1e+16,-0.0,1.2345678901234567,1.7976931348623157e+308,5e-324]\n"},
		{"[9007199254740993, -123456789012345678901234567890]", "[9007199254740993,-123456789012345678901234567890]\n"},
		{"{\"nested\": {\"z\": 2, \"a\": 1}}", "{\"nested\":{\"a\":1,\"z\":2}}\n"},
	}
	for _, item := range cases {
		actual, err := memberBytes([]byte(item.input))
		if err != nil || string(actual) != item.expected {
			t.Fatal(string(actual), item.expected, err)
		}
	}
}

func TestStrictJSONRejectsUnpairedUnicode(t *testing.T) {
	for _, raw := range []string{`"\ud800"`, `"\udc00"`, `{"key\ud800":1}`, `"\ud800\u0061"`} {
		var value any
		if StrictJSON([]byte(raw), &value) == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`"\ud83e\udd9a"`, `"literal \\ud800"`, `"鹈鹕"`} {
		var value any
		if err := StrictJSON([]byte(raw), &value); err != nil {
			t.Fatal(raw, err)
		}
	}
}
