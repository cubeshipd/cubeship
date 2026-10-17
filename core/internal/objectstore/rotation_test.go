package objectstore

import (
	"strings"
	"testing"
)

func TestManagedCredentialRotationUsesMiniAdminAndReplacesBothKeys(t *testing.T) {
	input := managedCredentialRotationInput("next_access", "next_secret")
	for _, expected := range []string{
		"-access_key=next_access",
		"-secret_key=next_secret",
		"-user=mini",
		"-actions=Admin,Read,List,Tagging,Write",
		"-apply",
	} {
		if !strings.Contains(input, expected) {
			t.Fatalf("rotation input %q missing %q", input, expected)
		}
	}
}
