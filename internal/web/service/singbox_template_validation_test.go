package service

import "testing"

func TestNormalizeSingBoxTemplateRejectsNonObject(t *testing.T) {
	for _, raw := range []string{"null", "[]", "42", `"value"`} {
		if _, err := normalizeSingBoxTemplate(raw); err == nil {
			t.Errorf("accepted invalid settings %s", raw)
		}
	}
	if _, err := normalizeSingBoxTemplate(`{"dns":{"servers":[]}}`); err != nil {
		t.Fatal(err)
	}
}
