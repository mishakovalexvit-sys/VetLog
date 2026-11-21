package lead

import "testing"

func TestValidate(t *testing.T) {
	valid := Input{Name: "Анна", Phone: "+7 (918) 000-00-00", Clinic: "Ветклиника Лапа", Consent: true}

	tests := []struct {
		name  string
		mut   func(*Input)
		field string
		want  string // "" — ошибок быть не должно
	}{
		{"valid", func(*Input) {}, "", ""},
		{"name empty", func(i *Input) { i.Name = "   " }, "name", ErrNameRequired},
		{"name too short", func(i *Input) { i.Name = "A" }, "name", ErrNameLength},
		{"name too long", func(i *Input) { i.Name = string(make([]rune, 101)) }, "name", ErrNameLength},
		{"phone letters", func(i *Input) { i.Phone = "abc" }, "phone", ErrPhoneInvalid},
		{"phone too short", func(i *Input) { i.Phone = "123" }, "phone", ErrPhoneInvalid},
		{"clinic empty", func(i *Input) { i.Clinic = "" }, "clinic", ErrClinicRequired},
		{"email invalid", func(i *Input) { i.Email = "a@" }, "email", ErrEmailInvalid},
		{"email with display name", func(i *Input) { i.Email = "Anna <a@b.ru>" }, "email", ErrEmailInvalid},
		{"email empty is ok", func(i *Input) { i.Email = "" }, "", ""},
		{"no consent", func(i *Input) { i.Consent = false }, "consent", ErrConsentRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.mut(&in)
			_, errs := Validate(in)
			if tt.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if got := errs[tt.field]; got != tt.want {
				t.Fatalf("field %s: got %q, want %q (all: %v)", tt.field, got, tt.want, errs)
			}
		})
	}
}

func TestValidateNormalizes(t *testing.T) {
	in, errs := Validate(Input{Name: " Анна ", Phone: "+7 (918) 000-00-00", Clinic: "Лапа", Consent: true})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if in.Name != "Анна" || in.Phone != "+79180000000" {
		t.Fatalf("normalized: name=%q phone=%q", in.Name, in.Phone)
	}
}
