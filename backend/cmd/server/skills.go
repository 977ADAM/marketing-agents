package main

import (
	briefservice "github.com/977ADAM/marketing-agents/internal/features/brief/service"
	campaignservice "github.com/977ADAM/marketing-agents/internal/features/campaign/service"
	reviewservice "github.com/977ADAM/marketing-agents/internal/features/review/service"
	topicservice "github.com/977ADAM/marketing-agents/internal/features/topic/service"
)

// skillBindings связывает роли агентов со скилами пакета marketing-skills.
// Собирается здесь, а не в адаптере: адаптеры не зависят от фич.
//
// Роли без скила в карте не значатся и уходят в модель как есть; карта покрывает
// все десять ролей, которые вызывают модель, чтобы промпты агентов собирались из
// одного источника.
func skillBindings() map[string]string {
	return map[string]string{
		// Стратег планирует кампанию по скилу campaign-plan.
		campaignservice.RoleStrategist: "campaign-plan",
		// Копирайтер пишет нативную статью, критик её проверяет.
		campaignservice.RoleCopywriter: "native-article",
		campaignservice.RoleCritic:     "article-review",
		// Проверки ревью (соответствие брифу и качество текста) — тот же скил.
		reviewservice.RoleCompliance: "article-review",
		reviewservice.RoleQuality:    "article-review",
		// Семантический подбор тем идёт по скилу планирования кампании.
		topicservice.RoleSeeds:    "campaign-plan",
		topicservice.RoleCluster:  "campaign-plan",
		topicservice.RoleSelect:   "campaign-plan",
		topicservice.RoleFallback: "campaign-plan",
		// Интервьюер собирает контекст кампании и отвечает прозой.
		briefservice.RoleInterviewer: "campaign-context",
	}
}
