package lead

import (
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ErrNameRequired    = "name.required"
	ErrNameLength      = "name.length"
	ErrPhoneInvalid    = "phone.invalid"
	ErrClinicRequired  = "clinic.required"
	ErrClinicLength    = "clinic.length"
	ErrEmailInvalid    = "email.invalid"
	ErrConsentRequired = "consent.required"
)

// ErrorCodes — все коды ошибок. Тест i18n проверяет перевод для каждого из них.
var ErrorCodes = []string{
	ErrNameRequired, ErrNameLength, ErrPhoneInvalid, ErrClinicRequired,
	ErrClinicLength, ErrEmailInvalid, ErrConsentRequired,
}

type Input struct {
	Name    string
	Phone   string
	Clinic  string
	Email   string
	Consent bool
}

// Errors: имя поля -> код ошибки.
type Errors map[string]string

type Lead struct {
	ID        int64
	Name      string
	Phone     string
	Clinic    string
	Email     string
	Lang      string
	CreatedAt time.Time
}

type Consent struct {
	AcceptedAt time.Time
	IP         string
	UserAgent  string
}

var (
	phoneRe   = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
	phoneSepR = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
)

// Validate нормализует ввод (trim, телефон) и возвращает ошибки по полям.
func Validate(in Input) (Input, Errors) {
	in.Name = strings.TrimSpace(in.Name)
	in.Clinic = strings.TrimSpace(in.Clinic)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = phoneSepR.Replace(strings.TrimSpace(in.Phone))

	errs := Errors{}

	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		errs["name"] = ErrNameRequired
	case n < 2 || n > 100:
		errs["name"] = ErrNameLength
	}

	if !phoneRe.MatchString(in.Phone) {
		errs["phone"] = ErrPhoneInvalid
	}

	switch n := utf8.RuneCountInString(in.Clinic); {
	case n == 0:
		errs["clinic"] = ErrClinicRequired
	case n < 2 || n > 200:
		errs["clinic"] = ErrClinicLength
	}

	if in.Email != "" {
		addr, err := mail.ParseAddress(in.Email)
		// Отсекаем формат "Имя <a@b>": принимаем только голый адрес.
		if err != nil || addr.Address != in.Email || len(in.Email) > 254 {
			errs["email"] = ErrEmailInvalid
		}
	}

	if !in.Consent {
		errs["consent"] = ErrConsentRequired
	}
	return in, errs
}
