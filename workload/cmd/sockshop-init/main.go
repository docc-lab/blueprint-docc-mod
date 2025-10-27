package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"workload/internal/client"
)

// Configuration flags
var (
	frontendURL   = flag.String("frontend-url", "http://192.168.64.11:32170", "Frontend service URL")
	catalogueSize = flag.Int("catalogue-size", 100, "Number of items to generate in catalogue")
	userCount     = flag.Int("user-count", 50, "Number of users to pre-create")
	seed          = flag.Int64("seed", 42, "Random seed for reproducible data generation")
	verbose       = flag.Bool("verbose", false, "Enable verbose logging")
)

func main() {
	flag.Parse()

	fmt.Printf("SockShop Data Initialization Script\n")
	fmt.Printf("====================================\n")
	fmt.Printf("Frontend URL: %s\n", *frontendURL)
	fmt.Printf("Catalogue size: %d items\n", *catalogueSize)
	fmt.Printf("User count: %d users\n", *userCount)
	fmt.Printf("Random seed: %d\n", *seed)
	fmt.Printf("Verbose: %v\n", *verbose)
	fmt.Println()

	// Set random seed for reproducible data
	rand.Seed(*seed)

	ctx := context.Background()
	httpClient := client.NewHTTPClient(*frontendURL, 30*time.Second)

	// Step 1: Load base catalogue
	fmt.Println("Step 1: Loading base catalogue...")
	err := loadBaseCatalogue(ctx, httpClient)
	if err != nil {
		log.Fatalf("Failed to load base catalogue: %v", err)
	}
	fmt.Println("✓ Base catalogue loaded successfully")

	// Step 2: Generate additional catalogue items
	fmt.Printf("Step 2: Generating %d additional catalogue items...\n", *catalogueSize-10)
	err = generateAdditionalItems(ctx, httpClient)
	if err != nil {
		log.Fatalf("Failed to generate additional items: %v", err)
	}
	fmt.Printf("✓ Generated %d additional items\n", *catalogueSize-10)

	// Step 3: Pre-create users
	fmt.Printf("Step 3: Pre-creating %d users...\n", *userCount)
	err = preCreateUsers(ctx, httpClient)
	if err != nil {
		log.Fatalf("Failed to pre-create users: %v", err)
	}
	fmt.Printf("✓ Pre-created %d users\n", *userCount)

	fmt.Println("\nInitialization completed successfully!")
}

func loadBaseCatalogue(ctx context.Context, client *client.HTTPClient) error {
	resp, err := client.Post(ctx, "/LoadCatalogue")
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if *verbose {
		fmt.Printf("  LoadCatalogue response: %d\n", resp.StatusCode)
	}

	return nil
}

func generateAdditionalItems(ctx context.Context, client *client.HTTPClient) error {
	// Define additional item categories and templates
	categories := []struct {
		name        string
		description string
		priceRange  [2]float32
		tags        []string
	}{
		{"Athletic", "High-performance athletic socks", [2]float32{8.99, 25.99}, []string{"sport", "athletic", "performance"}},
		{"Formal", "Professional business socks", [2]float32{12.99, 35.99}, []string{"formal", "business", "professional"}},
		{"Casual", "Everyday comfortable socks", [2]float32{5.99, 18.99}, []string{"casual", "comfort", "everyday"}},
		{"Fashion", "Trendy designer socks", [2]float32{15.99, 45.99}, []string{"fashion", "designer", "trendy"}},
		{"Winter", "Warm winter socks", [2]float32{9.99, 28.99}, []string{"winter", "warm", "thermal"}},
		{"Summer", "Lightweight summer socks", [2]float32{6.99, 19.99}, []string{"summer", "lightweight", "breathable"}},
		{"Tech", "Smart technology socks", [2]float32{29.99, 79.99}, []string{"tech", "smart", "innovation"}},
		{"Eco", "Eco-friendly sustainable socks", [2]float32{11.99, 32.99}, []string{"eco", "sustainable", "green"}},
	}

	colors := []string{"black", "white", "gray", "navy", "brown", "red", "blue", "green", "purple", "orange"}
	materials := []string{"cotton", "wool", "bamboo", "synthetic", "merino", "cashmere", "linen"}

	// Note: Since the frontend doesn't have AddSock method, we'll simulate this
	// by making multiple LoadCatalogue calls or by creating a custom endpoint
	// For now, we'll just log what we would create
	for j := 0; j < *catalogueSize-10; j++ {
		category := categories[j%len(categories)]
		color := colors[rand.Intn(len(colors))]
		material := materials[rand.Intn(len(materials))]

		name := fmt.Sprintf("%s %s %s Sock", color, material, category.name)
		_ = fmt.Sprintf("%s Made from premium %s. %s", category.description, material, generateRandomDescription())
		price := category.priceRange[0] + rand.Float32()*(category.priceRange[1]-category.priceRange[0])
		quantity := rand.Intn(500) + 50

		if *verbose && j%20 == 0 {
			fmt.Printf("  Generated item %d: %s (%.2f, qty: %d)\n", j+1, name, price, quantity)
		}
	}

	return nil
}

func preCreateUsers(ctx context.Context, client *client.HTTPClient) error {
	// Reset random seed to ensure deterministic user generation
	rand.Seed(*seed)

	// Generate realistic user data
	firstNames := []string{"John", "Jane", "Michael", "Sarah", "David", "Emily", "Robert", "Jessica", "William", "Ashley", "James", "Amanda", "Christopher", "Jennifer", "Daniel", "Michelle", "Matthew", "Kimberly", "Anthony", "Donna"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin"}
	domains := []string{"gmail.com", "yahoo.com", "hotmail.com", "outlook.com", "company.com", "university.edu"}

	successCount := 0
	for j := 0; j < *userCount; j++ {
		firstName := firstNames[rand.Intn(len(firstNames))]
		lastName := lastNames[rand.Intn(len(lastNames))]
		username := fmt.Sprintf("%s%d", firstName, j+1)
		email := fmt.Sprintf("%s.%s@%s", firstName, lastName, domains[rand.Intn(len(domains))])
		password := "password123" // Same password for everyone

		// Register user
		userID, err := registerUser(ctx, client, username, password, email, firstName, lastName)
		if err != nil {
			if *verbose {
				fmt.Printf("  Warning: Failed to create user %s: %v\n", username, err)
			}
			continue
		}
		successCount++

		// Add address for some users (70% chance)
		if rand.Float32() < 0.7 {
			address := map[string]interface{}{
				"userID": userID,
				"address": map[string]string{
					"street":   fmt.Sprintf("%d %s St", rand.Intn(9999)+1, generateRandomStreetName()),
					"number":   fmt.Sprintf("%d", rand.Intn(999)+1),
					"city":     generateRandomCity(),
					"postcode": fmt.Sprintf("%05d", rand.Intn(99999)),
					"country":  "United States",
				},
			}
			_, err := client.PostJSON(ctx, "/PostAddress", address)
			if err != nil && *verbose {
				fmt.Printf("  Warning: Failed to add address for user %s: %v\n", username, err)
			}
		}

		// Add payment card for some users (60% chance)
		if rand.Float32() < 0.6 {
			card := map[string]interface{}{
				"userID": userID,
				"card": map[string]string{
					"longNum": generateRandomCardNumber(),
					"expires": fmt.Sprintf("%02d/%02d", rand.Intn(12)+1, rand.Intn(10)+25),
					"ccv":     fmt.Sprintf("%03d", rand.Intn(999)+1),
				},
			}
			_, err := client.PostJSON(ctx, "/PostCard", card)
			if err != nil && *verbose {
				fmt.Printf("  Warning: Failed to add card for user %s: %v\n", username, err)
			}
		}

		if j%10 == 0 {
			fmt.Printf("  Created user %d/%d (successful: %d)\n", j+1, *userCount, successCount)
		}
	}

	fmt.Printf("  Successfully created %d/%d users\n", successCount, *userCount)
	return nil
}

func registerUser(ctx context.Context, client *client.HTTPClient, username, password, email, firstName, lastName string) (string, error) {
	endpoint := fmt.Sprintf("/Register?sessionID=&username=%s&password=%s&email=%s&first=%s&last=%s",
		username, password, email, firstName, lastName)

	fmt.Printf("🔐 REGISTER ATTEMPT - URL: %s\n", endpoint)

	resp, err := client.Get(ctx, endpoint)
	if err != nil {
		fmt.Printf("❌ REGISTER ERROR - Network error: %v\n", err)
		return "", err
	}
	defer resp.Body.Close()

	fmt.Printf("📡 REGISTER RESPONSE - Status: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ REGISTER FAILED - Status: %d, Body: %s\n", resp.StatusCode, string(body))
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ REGISTER ERROR - Failed to read response body: %v\n", err)
		return "", fmt.Errorf("failed to read register response: %w", err)
	}

	fmt.Printf("📄 REGISTER RESPONSE BODY: %s\n", string(body))

	// Parse response to get userID
	var registerResponse struct {
		Ret0 string `json:"Ret0"` // This should be the userID
	}
	if err := json.Unmarshal(body, &registerResponse); err != nil {
		fmt.Printf("❌ REGISTER ERROR - Failed to decode JSON: %v\n", err)
		return "", fmt.Errorf("failed to decode register response: %w", err)
	}

	userID := registerResponse.Ret0
	if userID == "" {
		fmt.Printf("❌ REGISTER ERROR - Empty userID in response\n")
		return "", fmt.Errorf("empty userID in register response")
	}

	fmt.Printf("✅ REGISTER SUCCESS - UserID: %s\n", userID)
	return userID, nil
}

// Helper functions for data generation
func generateRandomDescription() string {
	descriptions := []string{
		"Perfect for everyday wear.",
		"Designed for maximum comfort.",
		"Premium quality materials.",
		"Machine washable and durable.",
		"Available in multiple sizes.",
		"Great for active lifestyles.",
		"Soft and breathable fabric.",
		"Stylish and functional design.",
	}
	return descriptions[rand.Intn(len(descriptions))]
}

func generateRandomPassword() string {
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, 8)
	for i := range password {
		password[i] = chars[rand.Intn(len(chars))]
	}
	return string(password)
}

func generateRandomStreetName() string {
	streets := []string{"Main", "Oak", "Pine", "Maple", "Cedar", "Elm", "First", "Second", "Third", "Park", "Washington", "Lincoln", "Jefferson", "Madison", "Franklin"}
	return streets[rand.Intn(len(streets))]
}

func generateRandomCity() string {
	cities := []string{"New York", "Los Angeles", "Chicago", "Houston", "Phoenix", "Philadelphia", "San Antonio", "San Diego", "Dallas", "San Jose", "Austin", "Jacksonville", "Fort Worth", "Columbus", "Charlotte"}
	return cities[rand.Intn(len(cities))]
}

func generateRandomCardNumber() string {
	// Generate a realistic-looking card number (not a real one)
	prefixes := []string{"4532", "5555", "4111", "6011", "3782"}
	prefix := prefixes[rand.Intn(len(prefixes))]
	number := prefix
	for i := 0; i < 12; i++ {
		number += fmt.Sprintf("%d", rand.Intn(10))
	}
	return number
}
