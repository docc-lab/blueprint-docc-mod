package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"time"

	"workload/internal/client"
	"workload/internal/metrics"
)

// SockShopWorkflow implements deep workflows for SockShop application
type SockShopWorkflow struct {
	client  *client.HTTPClient
	stats   *metrics.StatsCollector
	verbose bool
}

// NewSockShopWorkflow creates a new SockShop workflow instance
func NewSockShopWorkflow(baseURL string, stats *metrics.StatsCollector, verbose bool) *SockShopWorkflow {
	// Use a much longer timeout (5 minutes) to prevent premature timeouts under high load
	// The context cancellation will handle stopping requests when the workload duration expires
	return &SockShopWorkflow{
		client:  client.NewHTTPClientWithVerbose(baseURL, 5*time.Minute, verbose),
		stats:   stats,
		verbose: verbose,
	}
}

// UserSession represents a user session for workflow testing
type UserSession struct {
	ID        string // This will be the username initially, then the userID after login
	Username  string
	Password  string
	Email     string
	Cart      []CartItem
	Orders    []string
	UserID    string // The actual database userID returned from login
	AddressID string // Cached address ID to avoid recreating
	CardID    string // Cached card ID to avoid recreating
}

type CartItem struct {
	ID        string  `json:"id"`
	Quantity  int     `json:"quantity"`
	UnitPrice float32 `json:"unit_price"`
}

type Sock struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       float32  `json:"price"`
	Quantity    int      `json:"quantity"`
	Tags        []string `json:"tag"`
}

// WorkflowType defines different types of workflows
type WorkflowType string

const (
	RealisticWorkflow  WorkflowType = "realistic"
	BrowsingWorkflow   WorkflowType = "browsing"
	PurchasingWorkflow WorkflowType = "purchasing"
	StressWorkflow     WorkflowType = "stress"
)

// RunWorkflow executes a specific workflow type for a user session
// stopChan signals when no new work should be started (allows ongoing work to complete)
func (w *SockShopWorkflow) RunWorkflow(ctx context.Context, stopChan <-chan struct{}, session *UserSession, workflowType WorkflowType) {
	switch workflowType {
	case RealisticWorkflow:
		w.runRealisticWorkflow(ctx, stopChan, session)
	case BrowsingWorkflow:
		w.runBrowsingWorkflow(ctx, stopChan, session)
	case PurchasingWorkflow:
		w.runPurchasingWorkflow(ctx, stopChan, session)
	case StressWorkflow:
		w.runStressWorkflow(ctx, stopChan, session)
	default:
		w.runRealisticWorkflow(ctx, stopChan, session)
	}
}

// runRealisticWorkflow implements a realistic e-commerce workflow
func (w *SockShopWorkflow) runRealisticWorkflow(ctx context.Context, stopChan <-chan struct{}, session *UserSession) {
	for {
		// Check if we should stop starting new work (but allow ongoing work to complete)
		select {
		case <-stopChan:
			return // No new work should start, exit the loop
		default:
		}

			// Record the start of a workload iteration
			w.stats.RecordWorkloadStart()
			
			// Step 1: Login to get the actual userID
			userID, err := w.loginUser(ctx, session.Username, session.Password)
			if err != nil {
				if w.verbose {
					fmt.Printf("❌ LOGIN FAILED! Username: %s, Error: %v\n", session.Username, err)
				}
				time.Sleep(2 * time.Second)
				continue
			}
			session.UserID = userID // Store the actual database userID

			if w.verbose {
				fmt.Printf("✅ LOGIN SUCCESS! Username: %s, UserID: %s\n", session.Username, userID)
			}

			// Step 2: Generate new session for fresh cart
			w.generateNewSession(ctx, session)
			if w.verbose {
				fmt.Printf("🛒 NEW CART SESSION: %s\n", session.ID)
			}

			// Step 3: Browse catalogue
			w.browseCatalogue(ctx, session)
			time.Sleep(1 * time.Second)

			// Step 4: Add items to cart
			cartSessionID := w.addToCart(ctx, session)
			time.Sleep(1 * time.Second)

			// Step 5: Add address and payment method (only if not already done)
			var addressID, cardID string
			if session.AddressID == "" || session.CardID == "" {
				addressID, cardID = w.addAddressAndPayment(ctx, session)
				if addressID == "" || cardID == "" {
					if w.verbose {
						fmt.Printf("❌ Failed to add address/payment for user %s\n", session.Username)
					}
					time.Sleep(2 * time.Second)
					continue
				}
				session.AddressID = addressID
				session.CardID = cardID
			} else {
				addressID = session.AddressID
				cardID = session.CardID
			}

			// Step 6: Place order
			orderID := w.placeOrder(ctx, session, addressID, cardID, cartSessionID)
			if orderID == "" {
				if w.verbose {
					fmt.Printf("❌ Failed to place order for user %s\n", session.Username)
				}
			} else {
				session.Orders = append(session.Orders, orderID)
				if w.verbose {
					fmt.Printf("✅ ORDER PLACED! User: %s, OrderID: %s\n", session.Username, orderID)
				}
			}

			// Step 7: Check order status
			w.getOrders(ctx, session)
			time.Sleep(1 * time.Second)

			// Think time between complete workflows
			time.Sleep(3 * time.Second)
	}
}

// runBrowsingWorkflow implements a heavy browsing workflow
func (w *SockShopWorkflow) runBrowsingWorkflow(ctx context.Context, stopChan <-chan struct{}, session *UserSession) {
	for {
		// Check if we should stop starting new work (but allow ongoing work to complete)
		select {
		case <-stopChan:
			return // No new work should start, exit the loop
		default:
		}

			// Record the start of a workload iteration
			w.stats.RecordWorkloadStart()
			
			// Browse by tags (2 hops: Frontend → Catalogue)
			w.browseByTags(ctx, session)

			// Search for specific items (2 hops: Frontend → Catalogue)
			w.searchItems(ctx, session)

			// Get item details (2 hops: Frontend → Catalogue)
			w.getItemDetails(ctx, session)

			time.Sleep(1 * time.Second)
	}
}

// runPurchasingWorkflow implements a heavy purchasing workflow
func (w *SockShopWorkflow) runPurchasingWorkflow(ctx context.Context, stopChan <-chan struct{}, session *UserSession) {
	for {
		// Check if we should stop starting new work (but allow ongoing work to complete)
		select {
		case <-stopChan:
			return // No new work should start, exit the loop
		default:
		}

			// Record the start of a workload iteration
			w.stats.RecordWorkloadStart()
			
			// Quick browse and add to cart
			w.browseCatalogue(ctx, session)
			w.addToCart(ctx, session)

			// Login to get userID
			userID, err := w.loginUser(ctx, session.Username, session.Password)
			if err == nil {
				session.UserID = userID // Store the actual database userID
			}
			w.addAddressAndPayment(ctx, session)

			// Place multiple orders
			for i := 0; i < 3; i++ {
				if len(session.Cart) > 0 {
					addressID, cardID := w.addAddressAndPayment(ctx, session)
					cartSessionID := w.addToCart(ctx, session)
					w.placeOrder(ctx, session, addressID, cardID, cartSessionID)
				}
			}

			time.Sleep(1 * time.Second)
	}
}

// runStressWorkflow implements a stress testing workflow
func (w *SockShopWorkflow) runStressWorkflow(ctx context.Context, stopChan <-chan struct{}, session *UserSession) {
	iteration := 0
	for {
		// Check if we should stop starting new work (but allow ongoing work to complete)
		select {
		case <-stopChan:
			if w.verbose {
				fmt.Printf("🛑 Stress workflow completed after %d iterations\n", iteration)
			}
			return // No new work should start, exit the loop
		default:
		}

			iteration++
			// Record the start of a workload iteration
			w.stats.RecordWorkloadStart()
			
			if w.verbose && iteration%10 == 0 {
				fmt.Printf("🔄 Stress workflow iteration %d\n", iteration)
			}

			// Rapid-fire operations
			w.browseCatalogue(ctx, session)
			w.addToCart(ctx, session)
			userID, err := w.loginUser(ctx, session.Username, session.Password)
			if err == nil {
				session.UserID = userID // Store the actual database userID
			}
			addressID, cardID := w.addAddressAndPayment(ctx, session)
			cartSessionID := w.addToCart(ctx, session)
			w.placeOrder(ctx, session, addressID, cardID, cartSessionID)

			// Minimal think time for stress testing
			time.Sleep(100 * time.Millisecond)
	}
}

// Individual operation methods

func (w *SockShopWorkflow) browseCatalogue(ctx context.Context, session *UserSession) {
	start := time.Now()

	resp, err := w.client.Get(ctx, "/ListItems?tags=&order=&pageNum=1&pageSize=20")
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	// Log failed requests if verbose
	if w.verbose && !success {
		fmt.Printf("❌ FAILED: browse_catalogue for user %s - %s (HTTP %d) - %.2fms\n",
			session.ID, errorMsg, statusCode, float64(duration.Nanoseconds())/1e6)
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "browse_catalogue",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})
}

func (w *SockShopWorkflow) addToCart(ctx context.Context, session *UserSession) string {
	// First get some items to add
	items, err := w.getCatalogueItems(ctx)
	if err != nil || len(items) == 0 {
		return session.ID // Return current sessionID if no items
	}

	// Add random item to cart
	item := items[rand.Intn(len(items))]
	start := time.Now()

	endpoint := fmt.Sprintf("/AddItem?sessionID=%s&itemID=%s", session.ID, item.ID) // Use username for sessionID
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	// Log failed requests if verbose
	if w.verbose && !success {
		fmt.Printf("❌ FAILED: add_to_cart for user %s - %s (HTTP %d) - %.2fms\n",
			session.ID, errorMsg, statusCode, float64(duration.Nanoseconds())/1e6)
	}

	if success {
		// Parse response to get the sessionID
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			var addItemResponse struct {
				Ret0 string `json:"Ret0"` // This is the sessionID
			}
			if json.Unmarshal(body, &addItemResponse) == nil {
				// Update session.ID with the returned sessionID
				session.ID = addItemResponse.Ret0
			}
		}

		// Add to local cart tracking
		session.Cart = append(session.Cart, CartItem{
			ID:        item.ID,
			Quantity:  1,
			UnitPrice: item.Price,
		})
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "add_to_cart",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})

	// Return the sessionID for use as cartID
	return session.ID
}

func (w *SockShopWorkflow) loginUser(ctx context.Context, username, password string) (string, error) {
	endpoint := fmt.Sprintf("/Login?sessionID=&username=%s&password=%s", username, password)

	if w.verbose {
		fmt.Printf("🔐 LOGIN ATTEMPT - URL: %s\n", endpoint)
	}

	resp, err := w.client.Get(ctx, endpoint)
	if err != nil {
		if w.verbose {
			fmt.Printf("❌ LOGIN ERROR - Network error: %v\n", err)
		}
		return "", fmt.Errorf("failed to login user: %w", err)
	}
	defer resp.Body.Close()

	if w.verbose {
		fmt.Printf("📡 LOGIN RESPONSE - Status: %d\n", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		if w.verbose {
			fmt.Printf("❌ LOGIN FAILED - Status: %d, Body: %s\n", resp.StatusCode, string(body))
		}
		return "", fmt.Errorf("user login failed with status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if w.verbose {
			fmt.Printf("❌ LOGIN ERROR - Failed to read response body: %v\n", err)
		}
		return "", fmt.Errorf("failed to read login response: %w", err)
	}

	if w.verbose {
		fmt.Printf("📄 LOGIN RESPONSE BODY: %s\n", string(body))
	}

	var loginResponse struct {
		Ret0 string `json:"Ret0"` // This is the userID
		Ret1 struct {
			UserID string `json:"id"`
		} `json:"Ret1"` // This is the user object
	}
	if err := json.Unmarshal(body, &loginResponse); err != nil {
		if w.verbose {
			fmt.Printf("❌ LOGIN ERROR - Failed to decode JSON: %v\n", err)
		}
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}

	// Return the userID from either Ret0 or Ret1.id
	userID := ""
	if loginResponse.Ret0 != "" {
		userID = loginResponse.Ret0
	} else {
		userID = loginResponse.Ret1.UserID
	}

	if w.verbose {
		fmt.Printf("✅ LOGIN PARSED - UserID: %s\n", userID)
	}

	return userID, nil
}

func (w *SockShopWorkflow) addAddressAndPayment(ctx context.Context, session *UserSession) (string, string) {
	// Add address
	addressData := map[string]interface{}{
		"userID": session.UserID, // Use actual userID
		"address": map[string]string{
			"street":   "123 Test St",
			"number":   "123",
			"city":     "Test City",
			"postcode": "12345",
			"country":  "Test Country",
		},
	}

	start := time.Now()
	resp, err := w.client.PostJSON(ctx, "/PostAddress", addressData)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "add_address",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})

	// Add payment card
	cardData := map[string]interface{}{
		"userID": session.UserID, // Use actual userID
		"card": map[string]string{
			"longNum": "1234567890123456",
			"expires": "12/25",
			"ccv":     "123",
		},
	}

	start = time.Now()
	resp, err = w.client.PostJSON(ctx, "/PostCard", cardData)
	duration = time.Since(start)

	success = err == nil && resp != nil && resp.StatusCode == 200
	statusCode = 0
	errorMsg = ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "add_payment",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})

	// Use the actual userID directly as addressID and cardID
	addressID := session.UserID
	cardID := session.UserID

	return addressID, cardID
}

func (w *SockShopWorkflow) placeOrder(ctx context.Context, session *UserSession, addressID, cardID, cartSessionID string) string {
	start := time.Now()

	// Deep workflow: Frontend → Order → User → Cart → Payment → Shipping
	endpoint := fmt.Sprintf("/NewOrder?userID=%s&addressID=%s&cardID=%s&cartID=%s",
		session.UserID, addressID, cardID, cartSessionID) // Use cartSessionID returned from AddItem
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	// Log failed requests if verbose
	if w.verbose && !success {
		fmt.Printf("❌ FAILED: place_order for user %s - %s (HTTP %d) - %.2fms\n",
			session.ID, errorMsg, statusCode, float64(duration.Nanoseconds())/1e6)
	}

	if success {
		session.Orders = append(session.Orders, fmt.Sprintf("order_%d", len(session.Orders)+1))
		session.Cart = make([]CartItem, 0) // Cart should be emptied
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "place_order",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})

	// Return orderID if successful, empty string if failed
	if success {
		return fmt.Sprintf("order_%s_%d", session.UserID, time.Now().Unix())
	}
	return ""
}

func (w *SockShopWorkflow) getOrders(ctx context.Context, session *UserSession) {
	start := time.Now()

	endpoint := fmt.Sprintf("/GetOrders?userID=%s", session.UserID) // Use actual userID
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	// Log failed requests if verbose
	if w.verbose && !success {
		fmt.Printf("❌ FAILED: get_orders for user %s - %s (HTTP %d) - %.2fms\n",
			session.Username, errorMsg, statusCode, float64(duration.Nanoseconds())/1e6)
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "get_orders",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})
}

func (w *SockShopWorkflow) checkOrderStatus(ctx context.Context, session *UserSession) {
	if len(session.Orders) == 0 {
		return
	}

	start := time.Now()

	endpoint := fmt.Sprintf("/GetOrders?userID=%s", session.UserID) // Use actual userID
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	statusCode := 0
	errorMsg := ""

	if resp != nil {
		statusCode = resp.StatusCode
		resp.Body.Close()
	}

	if err != nil {
		errorMsg = err.Error()
	} else if !success {
		errorMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:     session.Username, // Use username for statistics
		Operation:  "check_orders",
		Duration:   duration,
		Success:    success,
		Error:      errorMsg,
		StatusCode: statusCode,
		Timestamp:  time.Now(),
	})
}

func (w *SockShopWorkflow) browseByTags(ctx context.Context, session *UserSession) {
	tags := []string{"formal", "casual", "sport", "geek", "fashion"}
	tag := tags[rand.Intn(len(tags))]

	start := time.Now()
	endpoint := fmt.Sprintf("/ListItems?tags=%s&order=&pageNum=1&pageSize=10", tag)
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	if resp != nil {
		resp.Body.Close()
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:    session.ID,
		Operation: "browse_by_tags",
		Duration:  duration,
		Success:   success,
		Timestamp: time.Now(),
	})
}

func (w *SockShopWorkflow) searchItems(ctx context.Context, session *UserSession) {
	start := time.Now()
	endpoint := "/ListItems?tags=&order=&pageNum=1&pageSize=5"
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	if resp != nil {
		resp.Body.Close()
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:    session.ID,
		Operation: "search_items",
		Duration:  duration,
		Success:   success,
		Timestamp: time.Now(),
	})
}

func (w *SockShopWorkflow) getItemDetails(ctx context.Context, session *UserSession) {
	// Get a random item ID to look up
	items, err := w.getCatalogueItems(ctx)
	if err != nil || len(items) == 0 {
		return
	}

	item := items[rand.Intn(len(items))]
	start := time.Now()

	endpoint := fmt.Sprintf("/GetSock?itemID=%s", item.ID)
	resp, err := w.client.Get(ctx, endpoint)
	duration := time.Since(start)

	success := err == nil && resp != nil && resp.StatusCode == 200
	if resp != nil {
		resp.Body.Close()
	}

	w.stats.RecordStat(metrics.RequestStat{
		UserID:    session.ID,
		Operation: "get_item_details",
		Duration:  duration,
		Success:   success,
		Timestamp: time.Now(),
	})
}

// Helper methods

func (w *SockShopWorkflow) getCatalogueItems(ctx context.Context) ([]Sock, error) {
	resp, err := w.client.Get(ctx, "/ListItems?tags=&order=&pageNum=1&pageSize=20")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse JSON response
	var result struct {
		Ret0 []Sock `json:"Ret0"`
	}
	err = client.ParseJSONResponse(resp, &result)
	if err != nil {
		return nil, err
	}

	return result.Ret0, nil
}

// generateNewSession creates a new sessionID for fresh cart operations
func (w *SockShopWorkflow) generateNewSession(ctx context.Context, session *UserSession) {
	// Generate a new sessionID - we can use a simple UUID-like approach
	// or call a "new session" endpoint if SockShop has one
	session.ID = fmt.Sprintf("session_%d_%d", time.Now().UnixNano(), rand.Intn(10000))

	if w.verbose {
		fmt.Printf("🆕 Generated new sessionID: %s\n", session.ID)
	}
}
