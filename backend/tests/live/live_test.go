package live_test

import (
	"context"
	"os"
	"testing"
	"time"

	topic "github.com/977ADAM/marketing-agents/internal/features/topic/domain"
	wordstat "github.com/977ADAM/marketing-agents/internal/features/topic/source/wordstat"
)

// TestLiveMCP — дымовой тест против настоящего MCP-сервера. По умолчанию
// пропускается: он ходит в сеть и тратит квоту Wordstat, поэтому запускается
// только явно, с переменными окружения:
//
//	WORDSTAT_MCP_URL=https://... WORDSTAT_MCP_USER=... WORDSTAT_MCP_PASS=... \
//	    go test ./tests/live/ -run TestLiveMCP -v
func TestLiveMCP(t *testing.T) {
	url := os.Getenv("WORDSTAT_MCP_URL")
	if url == "" {
		t.Skip("WORDSTAT_MCP_URL не задан — живой прогон пропущен")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c := wordstat.New(wordstat.Options{
		URL:  url,
		User: os.Getenv("WORDSTAT_MCP_USER"),
		Pass: os.Getenv("WORDSTAT_MCP_PASS"),
	})

	// Спрос по стране и по Москве: регион должен реально сужать выборку.
	all, err := c.Demand(ctx, topic.DemandParams{Phrase: "зимняя резина", NumPhrases: 5})
	if err != nil {
		t.Fatalf("TopRequests (вся Россия): %v", err)
	}
	if !all.HasData || all.TotalCount <= 0 {
		t.Fatalf("ожидался спрос по фразе, получили %+v", all)
	}
	if len(all.Requests) == 0 {
		t.Error("нет популярных запросов")
	}

	moscow, err := c.Demand(ctx, topic.DemandParams{Phrase: "зимняя резина", NumPhrases: 5, Regions: []string{"213"}})
	if err != nil {
		t.Fatalf("TopRequests (Москва): %v", err)
	}
	if moscow.TotalCount >= all.TotalCount {
		t.Errorf("спрос по Москве (%d) должен быть меньше общероссийского (%d)", moscow.TotalCount, all.TotalCount)
	}

	dyn, err := c.Dynamics(ctx, topic.DynamicsParams{Phrase: "зимняя резина", Period: "monthly"})
	if err != nil {
		t.Fatalf("Dynamics: %v", err)
	}
	if len(dyn.Points) < 6 {
		t.Errorf("точек динамики %d, ожидалось не меньше 6", len(dyn.Points))
	}

	regions, err := c.Regions(ctx, wordstat.RegionsParams{Phrase: "аренда офиса", RegionMode: "regions", IncludeNames: true})
	if err != nil {
		t.Fatalf("Regions: %v", err)
	}
	if len(regions.Items) == 0 {
		t.Fatal("нет распределения по регионам")
	}
	for i := 1; i < len(regions.Items); i++ {
		if regions.Items[i-1].Count < regions.Items[i].Count {
			t.Errorf("регионы не отсортированы по убыванию: %d < %d", regions.Items[i-1].Count, regions.Items[i].Count)
			break
		}
	}
	if regions.Items[0].Name == "" {
		t.Error("includeNames не вернул названия регионов")
	}

	t.Logf("вся Россия: totalCount=%d, requests=%d, associations=%d",
		all.TotalCount, len(all.Requests), len(all.Associations))
	t.Logf("Москва: totalCount=%d (%.0f%% от страны)", moscow.TotalCount,
		100*float64(moscow.TotalCount)/float64(all.TotalCount))
	t.Logf("динамика: %d точек, первая %s = %d", len(dyn.Points), dyn.Points[0].Date, dyn.Points[0].Count)
	t.Logf("регионы: %d, лидер %s (%s) count=%d affinity=%.0f",
		len(regions.Items), regions.Items[0].RegionID, regions.Items[0].Name,
		regions.Items[0].Count, regions.Items[0].AffinityIndex)
}
