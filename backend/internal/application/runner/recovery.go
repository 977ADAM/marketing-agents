package runner

import "context"

type Recoverer interface {
	RecoverInterrupted(context.Context) (int64, error)
}

func RecoverInterrupted(ctx context.Context, stores ...Recoverer) (int64, error) {
	var total int64
	for _, s := range stores {
		n, err := s.RecoverInterrupted(ctx)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
