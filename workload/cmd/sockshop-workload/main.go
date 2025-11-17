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
	thinkTime   = flag.Duration("think-time", 0*time.Second, "Think time between requests")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	outputFile  = flag.String("output", "workload_stats.json", "Output file for statistics")
	workloadMix = flag.String("mix", "realistic", "Workload mix: realistic, browsing, purchasing, stress")
	seed        = flag.Int64("seed", 42, "Random seed for reproducible behavior")
)

func main() {
	flag.Parse()

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

	// Initialize statistics collector
	stats := metrics.NewStatsCollector()

	// Create context without deadline - allows ongoing requests to complete
	ctx := context.Background()

	// Create a stop channel that signals when no new work should be started
	// This allows ongoing work to complete while preventing new iterations
	stopChan := make(chan struct{})
	go func() {
		time.Sleep(*duration)
		close(stopChan) // Signal that no new work should start
	}()

	// Pre-generate all user data before starting worker threads
	// Reset seed once like the init script does, then generate all users in sequence
	fmt.Printf("Pre-generating user data for %d users...\n", *numUsers)
	// The init script: rand.Seed(*seed) in main(), then generateAdditionalItems consumes random numbers,
	// then preCreateUsers calls rand.Seed(*seed) again to reset before generating users.
	// generateAllUsers will reset the seed internally, matching preCreateUsers behavior.
	preGeneratedUsers := generateAllUsers(*numUsers)

	// Start workload
	fmt.Printf("Starting workload with %d users...\n", *numUsers)

	// Run concurrent users
	var wg sync.WaitGroup

	for i := 0; i < *numUsers; i++ {
		wg.Add(1)
		go func(userIndex int) {
			defer wg.Done()
			runUserWorkload(ctx, stopChan, userIndex, stats, preGeneratedUsers[userIndex])
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

func runUserWorkload(ctx context.Context, stopChan <-chan struct{}, userIndex int, stats *metrics.StatsCollector, preCreatedUser PreCreatedUser) {
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

	// Run the workflow with stop channel - allows ongoing work to complete
	workflow.RunWorkflow(ctx, stopChan, session, workflowType)
}

// PreCreatedUser represents a user created by the init script
type PreCreatedUser struct {
	ID       string
	Username string
	Password string
	Email    string
}

// generateAllUsers generates all users using the same algorithm as the init script
// This matches exactly how the init script generates users in preCreateUsers
// Uses a local RNG instance to ensure deterministic behavior isolated from global rand state
func generateAllUsers(count int) []PreCreatedUser {
	// Create a local RNG instance with the seed to ensure deterministic user generation
	// This isolates the RNG state from any other code that might use the global rand package
	rng := rand.New(rand.NewSource(*seed))

	// Use the same seed and algorithm as the init script to generate matching usernames
	firstNames := []string{"John", "Jane", "Michael", "Sarah", "David", "Emily", "Robert", "Jessica", "William", "Ashley", "James", "Amanda", "Christopher", "Jennifer", "Daniel", "Michelle", "Matthew", "Kimberly", "Anthony", "Donna"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin"}
	domains := []string{"gmail.com", "yahoo.com", "hotmail.com", "outlook.com", "company.com", "university.edu"}

	// Step 1: Pre-generate ONLY basic user info (matching init script Step 1)
	users := make([]PreCreatedUser, count)
	for j := 0; j < count; j++ {
		firstName := firstNames[rng.Intn(len(firstNames))]
		lastName := lastNames[rng.Intn(len(lastNames))]
		username := fmt.Sprintf("%s%d", firstName, j+1)
		email := fmt.Sprintf("%s.%s@%s", firstName, lastName, domains[rng.Intn(len(domains))])

		users[j] = PreCreatedUser{
			ID:       username,
			Username: username,
			Password: "password123",
			Email:    email,
		}
	}

	// Step 2: Loop through and determine addresses/cards (matching init script Step 2)
	// This consumes random numbers to match the init script's sequence
	for j := 0; j < count; j++ {
		_ = rng.Float32() < 0.7 // HasAddress - consume random number
		_ = rng.Float32() < 0.6 // HasCard - consume random number
	}

	return users
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
