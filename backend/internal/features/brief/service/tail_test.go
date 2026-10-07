package briefservice_test

import (
	"testing"

	brief "github.com/977ADAM/marketing-agents/internal/features/brief/domain"
	briefservice "github.com/977ADAM/marketing-agents/internal/features/brief/service"
)

// TestParseTail проверяет терпимый разбор машинного хвоста: маркер, код-фенс,
// проза со словом-маркером и битый JSON не должны ронять разговор.
func TestParseTail(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want brief.Draft
		ok   bool
	}{
		{
			name: "продукт после маркера",
			in:   "Текст\n<<<BRIEF\n{\"product\":\"Кружка\"}",
			want: brief.Draft{Product: "Кружка"},
			ok:   true,
		},
		{
			name: "код-фенс снят",
			in:   "Текст\n<<<BRIEF\n```json\n{\"goal\":\"Рост\"}\n```",
			want: brief.Draft{Goal: "Рост"},
			ok:   true,
		},
		{
			name: "маркера нет",
			in:   "Текст без маркера",
			want: brief.Draft{},
			ok:   false,
		},
		{
			name: "битый JSON",
			in:   "Текст\n<<<BRIEF\n{битый",
			want: brief.Draft{},
			ok:   false,
		},
		{
			// Если разбирать по первому вхождению, после маркера останется
			// проза и разбор провалится: этот вход различает первое и последнее.
			name: "разбор по последнему вхождению маркера",
			in:   "Проза со словом <<<BRIEF внутри и валидный JSON\n<<<BRIEF\n{\"product\":\"Кружка\"}",
			want: brief.Draft{Product: "Кружка"},
			ok:   true,
		},
		{
			name: "пустой объект",
			in:   "Текст\n<<<BRIEF\n{}",
			want: brief.Draft{},
			ok:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := briefservice.ParseTail(tt.in)
			if ok != tt.ok {
				t.Fatalf("ParseTail(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ParseTail(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseTailDoesNotPanic — разбор терпим к любому мусору: чат не должен
// рваться из-за неожиданного ответа модели.
func TestParseTailDoesNotPanic(t *testing.T) {
	inputs := []string{
		"",
		"<<<BRIEF",
		"<<<BRIEF\n",
		"<<<BRIEF\n   ",
		"<<<BRIEF\n```",
		"<<<BRIEF\n```json",
		"<<<BRIEF\nnull",
		"<<<BRIEF\n[1,2,3]",
		"<<<BRIEF\n\"строка\"",
		"<<<BRIEF\n{\"topics_count\":\"много\"}",
		"«<<<BRIEF»",
		"\x00\xff<<<BRIEF\n{",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseTail(%q) запаниковал: %v", in, r)
				}
			}()
			_, _ = briefservice.ParseTail(in)
		}()
	}
}

// TestMergeDraftKeepsKnownFields — частичный хвост не затирает уже собранное.
func TestMergeDraftKeepsKnownFields(t *testing.T) {
	prev := brief.Draft{
		Product: "Кружка", Goal: "Рост продаж", Audience: "ЗОЖ 25–40",
		Region: "225", TopicsCount: 3,
	}
	tail, ok := briefservice.ParseTail("Текст\n<<<BRIEF\n{\"tone\":\"дружелюбный\"}")
	if !ok {
		t.Fatal("хвост не разобран")
	}

	got := briefservice.MergeDraft(prev, tail)
	if got.Product != prev.Product || got.Goal != prev.Goal || got.Audience != prev.Audience {
		t.Errorf("слияние затёрло известные поля: %+v", got)
	}
	if got.Region != prev.Region || got.TopicsCount != prev.TopicsCount {
		t.Errorf("слияние затёрло необязательные поля: %+v", got)
	}
	if got.Tone != "дружелюбный" {
		t.Errorf("новое поле не применилось: %+v", got)
	}
}

// TestMergeDraftEmptyTailKeepsPrevious — пустой хвост (нет маркера или `{}`)
// оставляет прежний бриф как есть, включая пробельные значения.
func TestMergeDraftEmptyTailKeepsPrevious(t *testing.T) {
	prev := brief.Draft{Product: "Кружка", Tone: "строгий", TopicsCount: 2}
	tails := []struct {
		name string
		in   string
	}{
		{"нет маркера", "Текст без маркера"},
		{"битый JSON", "Текст\n<<<BRIEF\n{битый"},
		{"пустой объект", "Текст\n<<<BRIEF\n{}"},
		{"пробельные значения", "Текст\n<<<BRIEF\n{\"product\":\"   \",\"topics_count\":0}"},
	}
	for _, tt := range tails {
		t.Run(tt.name, func(t *testing.T) {
			// Неудачный разбор даёт нулевой хвост — слияние обязано сохранить
			// прежний бриф, ровно как и пустой разобранный объект.
			tail, _ := briefservice.ParseTail(tt.in)
			if got := briefservice.MergeDraft(prev, tail); got != prev {
				t.Errorf("слияние изменило бриф: %+v, want %+v", got, prev)
			}
		})
	}
}

// TestDraftHasRequired — обязательны ровно четыре поля брифа.
func TestDraftHasRequired(t *testing.T) {
	full := brief.Draft{Product: "Кружка", Goal: "Рост", Audience: "ЗОЖ", Tone: "дружелюбный"}
	if !full.HasRequired() {
		t.Error("полный бриф признан неполным")
	}
	for _, drop := range []func(brief.Draft) brief.Draft{
		func(d brief.Draft) brief.Draft { d.Product = ""; return d },
		func(d brief.Draft) brief.Draft { d.Goal = ""; return d },
		func(d brief.Draft) brief.Draft { d.Audience = ""; return d },
		func(d brief.Draft) brief.Draft { d.Tone = ""; return d },
		func(d brief.Draft) brief.Draft { d.Tone = "   "; return d },
	} {
		if drop(full).HasRequired() {
			t.Errorf("бриф без обязательного поля признан полным: %+v", drop(full))
		}
	}
	// Необязательные поля на готовность не влияют.
	if !(brief.Draft{Product: "п", Goal: "ц", Audience: "а", Tone: "т"}).HasRequired() {
		t.Error("необязательные поля помешали готовности")
	}
}
