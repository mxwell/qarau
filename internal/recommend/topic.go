package recommend

import "fmt"

type TopicRow struct {
	ID   int16
	Slug string
}

func SlugsToTopics(available map[string]int16, requestSlugs []string) ([]TopicRow, error) {
	result := make([]TopicRow, 0, len(requestSlugs))
	for _, slug := range requestSlugs {
		topicID, ok := available[slug]
		if !ok {
			return nil, fmt.Errorf("topic slug not found: %s", slug)
		}
		result = append(result, TopicRow{
			ID:   topicID,
			Slug: slug,
		})
	}
	return result, nil
}
