package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"workload/internal/metrics"
	"workload/internal/workflows"
)

// Configuration flags
var (
	frontendURL = flag.String("frontend-url", "http://192.168.64.11:32170", "Frontend service URL")
	numUsers    = flag.Int("users", 10, "Number of concurrent users to simulate")
	duration    = flag.Duration("duration", 5*time.Minute, "Duration to run the workload")
	thinkTime   = flag.Duration("think-time", 2*time.Second, "Think time between requests")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	outputFile  = flag.String("output", "workload_stats.json", "Output file for statistics")
	workloadMix = flag.String("mix", "realistic", "Workload mix: realistic, browsing, purchasing, stress")
	seed        = flag.Int64("seed", 42, "Random seed for reproducible behavior")
)

func main() {
	flag.Parse()
	rand.Seed(*seed) // Set random seed ONCE at the beginning

	fmt.Printf("SockShop E-commerce Workload Generator\n")
	fmt.Printf("=====================================\n")
	fmt.Printf("Frontend URL: %s\n", *frontendURL)
	fmt.Printf("Concurrent users: %d\n", *numUsers)
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Printf("Think time: %v\n", *thinkTime)
	fmt.Printf("Workload mix: %s\n", *workloadMix)
	fmt.Printf("Output file: %s\n", *outputFile)
	fmt.Printf("Random seed: %d\n", *seed)
	fmt.Printf("Verbose: %v\n", *verbose)
	fmt.Println()

	// Set random seed for reproducible behavior
	rand.Seed(*seed)

	// Initialize statistics collector
	stats := metrics.NewStatsCollector()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	// Pre-generate all user data before starting worker threads
	fmt.Printf("Pre-generating user data for %d users...\n", *numUsers)
	preGeneratedUsers := make([]PreCreatedUser, *numUsers)
	for i := 0; i < *numUsers; i++ {
		preGeneratedUsers[i] = getRandomPreCreatedUser(i)
		if *verbose {
			fmt.Printf("  Generated user %d: %s (%s)\n", i, preGeneratedUsers[i].Username, preGeneratedUsers[i].Email)
		}
	}

	// Start workload
	fmt.Printf("Starting workload with %d users...\n", *numUsers)

	// Run concurrent users
	var wg sync.WaitGroup

	for i := 0; i < *numUsers; i++ {
		wg.Add(1)
		go func(userIndex int) {
			defer wg.Done()
			runUserWorkload(ctx, userIndex, stats, preGeneratedUsers[userIndex])
		}(i)
	}

	// Wait for all users to complete
	wg.Wait()

	// Finalize statistics
	stats.Finalize()

	// Print results
	stats.PrintSummary()

	// Save results to file
	err := stats.SaveToFile(*outputFile)
	if err != nil {
		log.Printf("Failed to save results: %v", err)
	} else {
		fmt.Printf("\nStatistics saved to %s\n", *outputFile)
	}

	fmt.Println("\nWorkload completed successfully!")
}

func runUserWorkload(ctx context.Context, userIndex int, stats *metrics.StatsCollector, preCreatedUser PreCreatedUser) {
	// Create user session using the pre-generated user data
	session := &workflows.UserSession{
		ID:       preCreatedUser.ID,
		Username: preCreatedUser.Username,
		Password: preCreatedUser.Password,
		Email:    preCreatedUser.Email,
		Cart:     make([]workflows.CartItem, 0),
		Orders:   make([]string, 0),
	}

	// Create workflow instance
	workflow := workflows.NewSockShopWorkflow(*frontendURL, stats, *verbose)

	// Parse workflow type
	var workflowType workflows.WorkflowType
	switch *workloadMix {
	case "realistic":
		workflowType = workflows.RealisticWorkflow
	case "browsing":
		workflowType = workflows.BrowsingWorkflow
	case "purchasing":
		workflowType = workflows.PurchasingWorkflow
	case "stress":
		workflowType = workflows.StressWorkflow
	default:
		workflowType = workflows.RealisticWorkflow
	}

	// Run the workflow
	workflow.RunWorkflow(ctx, session, workflowType)
}

// PreCreatedUser represents a user created by the init script
type PreCreatedUser struct {
	ID       string
	Username string
	Password string
	Email    string
}

// getRandomPreCreatedUser returns a user from the pre-created users
// For now, we'll use the exact users we know were created by the init script
func getRandomPreCreatedUser(userID int) PreCreatedUser {
	// These are the exact users created by the init script with seed=42
	preCreatedUsers := []PreCreatedUser{
		{ID: "Emily1", Username: "Emily1", Password: "password123", Email: "Emily.Davis@hotmail.com"},
		{ID: "Ashley2", Username: "Ashley2", Password: "password123", Email: "Ashley.Davis@outlook.com"},
		{ID: "Michael3", Username: "Michael3", Password: "password123", Email: "Michael.Anderson@company.com"},
		{ID: "Daniel4", Username: "Daniel4", Password: "password123", Email: "Daniel.Gonzalez@outlook.com"},
		{ID: "William5", Username: "William5", Password: "password123", Email: "William.Taylor@yahoo.com"},
	}

	// Cycle through the pre-created users
	userIndex := userID % len(preCreatedUsers)
	user := preCreatedUsers[userIndex]

	fmt.Printf("Username: %s\n", user.Username)
	return user
}

// generateRandomPassword generates a random password (same as init script)
func generateRandomPassword() string {
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, 8)
	for i := range password {
		password[i] = chars[rand.Intn(len(chars))]
	}
	return string(password)
}
