package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
)

func main() {
	quotas, err := deploy.LoadQuotas("config/quotas.yaml")
	if err != nil {
		log.Fatal(err)
	}
	enforcer := deploy.NewQuotaEnforcer(quotas)
	var wg sync.WaitGroup
	waits := make(chan time.Duration, 8)
	profiles := []string{"personal", "organization"}
	for _, profile := range profiles {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(profile string) {
				defer wg.Done()
				start := time.Now()
				for {
					if err := enforcer.Acquire(profile, deploy.Usage{Workflows: 1, Runners: 1}); err == nil {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				waits <- time.Since(start)
				time.Sleep(20 * time.Millisecond)
				enforcer.Release(profile, deploy.Usage{Workflows: 1, Runners: 1})
			}(profile)
		}
	}
	wg.Wait()
	close(waits)
	var max time.Duration
	for wait := range waits {
		if wait > max {
			max = wait
		}
	}
	fmt.Printf("fairness soak complete: profiles=%v max_wait=%s\n", deploy.SortedProfiles(quotas), max)
}
