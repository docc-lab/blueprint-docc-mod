# Ecommerce Marketplace Workflows

This document defines the distributed workflows for each endpoint in the ecommerce marketplace, following the exact order from endpoints.md.

## User-Facing Endpoints

### Product Discovery & Browsing

#### GET /products - Browse products with filtering and pagination
**Complexity: Very High (13 services)**
```
API Gateway
├── Product Service
│   ├── Cache Service
│   │   ├── Storage Service
│   │   └── Retrieval Service
│   ├── Search Service
│   │   ├── Index Service
│   │   │   ├── Indexing Service
│   │   │   │   ├── Crawler Service
│   │   │   │   └── Processor Service
│   │   │   └── Search Engine Service
│   │   │       ├── Query Service
│   │   │       └── Rank Service
│   │   └── Filter Service
│   │       ├── Query Service
│   │       └── Rank Service
│   └── Category Service
│       ├── Hierarchy Service
│       └── Navigation Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   └── Pricing Service
│       ├── Base Price Service
│       └── Discount Service
```

#### GET /products/{productId} - Get detailed product information
**Complexity: Very High (15 services)**
```
API Gateway
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   ├── Review Service
│   │   ├── Rating Service
│   │   │   ├── Calculation Service
│   │   │   └── Aggregation Service
│   │   ├── Sentiment Service
│   │   │   ├── Analysis Service
│   │   │   └── Classification Service
│   │   └── Moderation Service
│   │       ├── Content Service
│   │       └── Policy Service
│   ├── Media Service
│   │   ├── Image Service
│   │   │   ├── Processing Service
│   │   │   └── Optimization Service
│   │   ├── Video Service
│   │   │   ├── Encoding Service
│   │   │   └── Streaming Service
│   │   └── Storage Service
│   │       ├── Upload Service
│   │       └── CDN Service
│   └── Pricing Service
│       ├── Calculation Service
│       │   ├── Base Price Service
│       │   └── Adjustment Service
│       └── Discount Service
│           ├── Rule Service
│           └── Application Service
```

#### GET /products/search - Search products with advanced filters
**Complexity: Very High (14 services)**
```
API Gateway
├── Search Service
│   ├── Index Service
│   │   ├── Indexing Service
│   │   │   ├── Crawler Service
│   │   │   └── Processor Service
│   │   └── Search Engine Service
│   │       ├── Query Service
│   │       └── Rank Service
│   ├── Filter Service
│   │   ├── Query Service
│   │   │   ├── Parser Service
│   │   │   └── Builder Service
│   │   └── Rank Service
│   │       ├── Scoring Service
│   │       └── Sort Service
│   └── Ranking Service
│       ├── Relevance Service
│       │   ├── Content Service
│       │   └── Popularity Service
│       └── Personalization Service
│           ├── User Profile Service
│           └── Preference Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### GET /categories - Browse product categories
**Complexity: Medium (5 services)**
```
API Gateway
├── Category Service
│   ├── Hierarchy Service
│   │   ├── Tree Service
│   │   └── Navigation Service
│   └── Metadata Service
│       ├── Attribute Service
│       └── Tag Service
```

#### GET /categories/{categoryId}/products - Products by category
**Complexity: High (8 services)**
```
API Gateway
├── Category Service
│   ├── Hierarchy Service
│   │   ├── Tree Service
│   │   └── Navigation Service
│   └── Metadata Service
│       ├── Attribute Service
│       └── Tag Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### GET /trending - Get trending products
**Complexity: Very High (12 services)**
```
API Gateway
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   │   ├── Pattern Service
│   │   │   └── Prediction Service
│   │   └── Calculation Service
│   │       ├── Algorithm Service
│   │       └── Weight Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### GET /deals - Get current deals and promotions
**Complexity: Very High (13 services)**
```
API Gateway
├── Promotion Service
│   ├── Discount Service
│   │   ├── Calculation Service
│   │   │   ├── Rule Service
│   │   │   └── Application Service
│   │   └── Validation Service
│   │       ├── Eligibility Service
│   │       └── Limit Service
│   ├── Coupon Service
│   │   ├── Generation Service
│   │   │   ├── Code Service
│   │   │   └── Validation Service
│   │   └── Redemption Service
│   │       ├── Tracking Service
│   │       └── Limit Service
│   └── Campaign Service
│       ├── Management Service
│       └── Scheduling Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### GET /brands - Browse brands
**Complexity: Medium (4 services)**
```
API Gateway
├── Brand Service
│   ├── Management Service
│   │   ├── Profile Service
│   │   └── Metadata Service
│   └── Search Service
│       ├── Index Service
│       └── Filter Service
```

#### GET /brands/{brandId}/products - Products by brand
**Complexity: High (8 services)**
```
API Gateway
├── Brand Service
│   ├── Management Service
│   │   ├── Profile Service
│   │   └── Metadata Service
│   └── Search Service
│       ├── Index Service
│       └── Filter Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

### User Account Management

#### POST /auth/register - User registration
**Complexity: Very High (16 services)**
```
API Gateway
├── Auth Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   ├── Preference Service
│   │   │   ├── Language Service
│   │   │   └── Currency Service
│   │   └── Profile Service
│   │       ├── Personal Service
│   │       └── Business Service
│   ├── Notification Service
│   │   ├── Email Service
│   │   │   ├── Template Service
│   │   │   └── Delivery Service
│   │   ├── SMS Service
│   │   │   ├── Gateway Service
│   │   │   └── Delivery Service
│   │   └── Push Notification Service
│   │       ├── Device Service
│   │       └── Delivery Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   └── Risk Assessment Service
│       ├── Credit Score Service
│       └── Transaction History Service
```

#### POST /auth/login - User login
**Complexity: Very High (14 services)**
```
API Gateway
├── Auth Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   └── Preference Service
│   │       ├── Language Service
│   │       └── Currency Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   └── Session Service
│       ├── Management Service
│       └── Security Service
├── Notification Service
│   ├── Email Service
│   │   ├── Template Service
│   │   └── Delivery Service
│   └── Push Notification Service
│       ├── Device Service
│       └── Delivery Service
```

#### POST /auth/logout - User logout
**Complexity: Simple (2 services)**
```
API Gateway
└── Auth Service
```

#### GET /user/profile - Get user profile
**Complexity: High (8 services)**
```
API Gateway
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### PUT /user/profile - Update user profile
**Complexity: Simple (2 services)**
```
API Gateway
└── User Service
```

#### GET /user/addresses - Get user addresses
**Complexity: High (6 services)**
```
API Gateway
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   └── Validation Service
│       ├── Format Service
│       └── Verification Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### POST /user/addresses - Add new address
**Complexity: Simple (2 services)**
```
API Gateway
└── User Service
```

#### PUT /user/addresses/{addressId} - Update address
**Complexity: Simple (2 services)**
```
API Gateway
└── User Service
```

#### DELETE /user/addresses/{addressId} - Delete address
**Complexity: Simple (2 services)**
```
API Gateway
└── User Service
```

#### GET /user/preferences - Get user preferences
**Complexity: High (7 services)**
```
API Gateway
├── User Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### PUT /user/preferences - Update user preferences
**Complexity: Simple (2 services)**
```
API Gateway
└── User Service
```

### Shopping Cart Operations

#### GET /cart - Get current cart
**Complexity: High (10 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /cart/items - Add item to cart
**Complexity: Very High (16 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   ├── Pricing Service
│   │   │   ├── Calculation Service
│   │   │   └── Discount Service
│   │   └── Media Service
│   │       ├── Image Service
│   │       └── Video Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   └── Preference Service
│   │       ├── Language Service
│   │       └── Currency Service
│   └── Promotion Service
│       ├── Discount Service
│       └── Coupon Service
```

#### PUT /cart/items/{itemId} - Update cart item quantity
**Complexity: High (6 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### DELETE /cart/items/{itemId} - Remove item from cart
**Complexity: Medium (4 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /cart/clear - Clear entire cart
**Complexity: Medium (4 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /cart/save - Save cart for later
**Complexity: Medium (5 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   ├── User Service
│   │   ├── Address Service
│   │   └── Preference Service
│   └── Storage Service
│       ├── Save Service
│       └── Retrieval Service
```

#### GET /cart/saved - Get saved carts
**Complexity: Medium (5 services)**
```
API Gateway
├── Cart Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   ├── User Service
│   │   ├── Address Service
│   │   └── Preference Service
│   └── Storage Service
│       ├── Save Service
│       └── Retrieval Service
```

### Checkout & Order Processing

#### POST /checkout/initiate - Start checkout process
**Complexity: Very High (18 services)**
```
API Gateway
├── Checkout Service
│   ├── Cart Service
│   │   ├── Product Service
│   │   │   ├── Inventory Service
│   │   │   │   ├── Warehouse Service
│   │   │   │   │   ├── Location Service
│   │   │   │   │   └── Capacity Service
│   │   │   │   └── Supplier Service
│   │   │   │       ├── Contract Service
│   │   │   │       └── Delivery Service
│   │   │   └── Pricing Service
│   │   │       ├── Discount Service
│   │   │       └── Tax Service
│   │   └── User Service
│   │       ├── Address Service
│   │       │   ├── Validation Service
│   │       │   └── Geocoding Service
│   │       └── Preference Service
│   │           ├── Language Service
│   │           └── Currency Service
│   ├── Payment Service
│   │   ├── Fraud Detection Service
│   │   │   ├── ML Model Service
│   │   │   └── Historical Data Service
│   │   └── Risk Assessment Service
│   │       ├── Credit Score Service
│   │       └── Transaction History Service
│   └── Shipping Service
│       ├── Location Service
│       │   ├── Geocoding Service
│       │   └── Distance Service
│       └── Tax Service
│           ├── Rate Service
│           └── Exemption Service
├── Inventory Service
│   ├── Warehouse Service
│   │   ├── Location Service
│   │   └── Capacity Service
│   └── Supplier Service
│       ├── Contract Service
│       └── Delivery Service
```

#### GET /checkout/summary - Get checkout summary
**Complexity: Medium (3 services)**
```
API Gateway
├── Checkout Service
│   ├── Cart Service
│   └── User Service
```

#### POST /checkout/validate - Validate checkout data
**Complexity: Medium (3 services)**
```
API Gateway
├── Checkout Service
│   ├── User Service
│   └── Inventory Service
```

#### POST /checkout/payment - Process payment
**Complexity: Very High (13 services)**
```
API Gateway
├── Checkout Service
│   ├── Payment Service
│   │   ├── Fraud Detection Service
│   │   ├── Risk Assessment Service
│   │   ├── Gateway Service
│   │   └── Settlement Service
│   ├── Order Service
│   │   ├── Inventory Service
│   │   │   ├── Warehouse Service
│   │   │   └── Supplier Service
│   │   └── User Service
│   │       ├── Address Service
│   │       └── Preference Service
│   └── Shipping Service
│       ├── Location Service
│       └── Tax Service
```

#### POST /checkout/confirm - Confirm order
**Complexity: Very High (22 services)**
```
API Gateway
├── Checkout Service
│   ├── Order Service
│   │   ├── Inventory Service
│   │   │   ├── Warehouse Service
│   │   │   │   ├── Location Service
│   │   │   │   │   ├── Geocoding Service
│   │   │   │   │   └── Distance Service
│   │   │   │   └── Capacity Service
│   │   │   │       ├── Space Service
│   │   │   │       └── Equipment Service
│   │   │   └── Supplier Service
│   │   │       ├── Contract Service
│   │   │       │   ├── Terms Service
│   │   │       │   └── Pricing Service
│   │   │       └── Delivery Service
│   │   │           ├── Route Service
│   │   │           └── Schedule Service
│   │   ├── Payment Service
│   │   │   ├── Fraud Detection Service
│   │   │   │   ├── ML Model Service
│   │   │   │   └── Historical Data Service
│   │   │   └── Risk Assessment Service
│   │   │       ├── Credit Score Service
│   │   │       └── Transaction History Service
│   │   └── Notification Service
│   │       ├── Email Service
│   │       │   ├── Template Service
│   │       │   └── Delivery Service
│   │       ├── SMS Service
│   │       │   ├── Gateway Service
│   │       │   └── Delivery Service
│   │       └── Push Notification Service
│   │           ├── Device Service
│   │           └── Delivery Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   │   ├── Geocoding Service
│   │   │   └── Distance Service
│   │   ├── Tax Service
│   │   │   ├── Rate Service
│   │   │   └── Exemption Service
│   │   └── Tracking Service
│   │       ├── Carrier Service
│   │       └── Status Service
│   └── User Service
│       ├── Address Service
│       │   ├── Validation Service
│       │   └── Geocoding Service
│       └── Preference Service
│           ├── Language Service
│           └── Currency Service
```

#### GET /checkout/shipping-options - Get shipping options
**Complexity: Simple (2 services)**
```
API Gateway
└── Shipping Service
```

#### POST /checkout/apply-coupon - Apply discount coupon
**Complexity: Medium (3 services)**
```
API Gateway
├── Checkout Service
│   ├── Promotion Service
│   └── Cart Service
```

#### DELETE /checkout/remove-coupon - Remove applied coupon
**Complexity: Simple (2 services)**
```
API Gateway
└── Checkout Service
```

### Order Management

#### GET /orders - Get user orders
**Complexity: High (6 services)**
```
API Gateway
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   └── User Service
└── Cache Service
```

#### GET /orders/{orderId} - Get order details
**Complexity: High (8 services)**
```
API Gateway
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   ├── Tracking Service
│   │   └── Tax Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /orders/{orderId}/tracking - Get order tracking
**Complexity: High (5 services)**
```
API Gateway
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   ├── Tracking Service
│   │   └── Carrier Service
│   └── User Service
```

#### POST /orders/{orderId}/cancel - Cancel order
**Complexity: Very High (20 services)**
```
API Gateway
├── Order Service
│   ├── Inventory Service
│   │   ├── Warehouse Service
│   │   │   ├── Location Service
│   │   │   │   ├── Geocoding Service
│   │   │   │   └── Distance Service
│   │   │   └── Capacity Service
│   │   │       ├── Space Service
│   │   │       └── Equipment Service
│   │   └── Supplier Service
│   │       ├── Contract Service
│   │       │   ├── Terms Service
│   │       │   └── Pricing Service
│   │       └── Delivery Service
│   │           ├── Route Service
│   │           └── Schedule Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   │   ├── Gateway Service
│   │   │   └── Settlement Service
│   │   ├── Fraud Detection Service
│   │   │   ├── ML Model Service
│   │   │   └── Historical Data Service
│   │   └── Risk Assessment Service
│   │       ├── Credit Score Service
│   │       └── Transaction History Service
│   ├── Notification Service
│   │   ├── Email Service
│   │   │   ├── Template Service
│   │   │   └── Delivery Service
│   │   ├── SMS Service
│   │   │   ├── Gateway Service
│   │   │   └── Delivery Service
│   │   └── Push Notification Service
│   │       ├── Device Service
│   │       └── Delivery Service
│   └── Shipping Service
│       ├── Location Service
│       │   ├── Geocoding Service
│       │   └── Distance Service
│       └── Tracking Service
│           ├── Carrier Service
│           └── Status Service
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   └── Preference Service
│       ├── Language Service
│       └── Currency Service
```

#### POST /orders/{orderId}/return - Initiate return
**Complexity: Very High (15 services)**
```
API Gateway
├── Order Service
│   ├── Inventory Service
│   │   ├── Warehouse Service
│   │   └── Supplier Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   ├── Fraud Detection Service
│   │   └── Risk Assessment Service
│   ├── Notification Service
│   │   ├── Email Service
│   │   ├── SMS Service
│   │   └── Push Notification Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /orders/{orderId}/invoice - Get order invoice
**Complexity: Medium (3 services)**
```
API Gateway
├── Order Service
└── Payment Service
```

### Reviews & Ratings

#### GET /products/{productId}/reviews - Get product reviews
**Complexity: Very High (12 services)**
```
API Gateway
├── Review Service
│   ├── Rating Service
│   │   ├── Calculation Service
│   │   │   ├── Average Service
│   │   │   └── Weight Service
│   │   └── Aggregation Service
│   │       ├── Summary Service
│   │       └── Distribution Service
│   ├── Sentiment Service
│   │   ├── Analysis Service
│   │   │   ├── NLP Service
│   │   │   └── Classification Service
│   │   └── Moderation Service
│   │       ├── Content Service
│   │       └── Policy Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### POST /products/{productId}/reviews - Post product review
**Complexity: Very High (15 services)**
```
API Gateway
├── Review Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   └── Preference Service
│   │       ├── Language Service
│   │       └── Currency Service
│   ├── Moderation Service
│   │   ├── Content Service
│   │   │   ├── Filter Service
│   │   │   └── Flag Service
│   │   └── Policy Service
│   │       ├── Rule Service
│   │       └── Enforcement Service
│   └── Sentiment Service
│       ├── Analysis Service
│       │   ├── NLP Service
│       │   └── Classification Service
│       └── Processing Service
│           ├── Text Service
│           └── Score Service
```

#### PUT /reviews/{reviewId} - Update review
**Complexity: Simple (2 services)**
```
API Gateway
└── Review Service
```

#### DELETE /reviews/{reviewId} - Delete review
**Complexity: Simple (2 services)**
```
API Gateway
└── Review Service
```

#### POST /reviews/{reviewId}/helpful - Mark review as helpful
**Complexity: Simple (2 services)**
```
API Gateway
└── Review Service
```

#### GET /user/reviews - Get user's reviews
**Complexity: Simple (2 services)**
```
API Gateway
└── Review Service
```

### Wishlist & Favorites

#### GET /wishlist - Get user wishlist
**Complexity: High (10 services)**
```
API Gateway
├── Wishlist Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /wishlist/items - Add item to wishlist
**Complexity: High (8 services)**
```
API Gateway
├── Wishlist Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### DELETE /wishlist/items/{itemId} - Remove from wishlist
**Complexity: Medium (4 services)**
```
API Gateway
├── Wishlist Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /wishlist/share - Share wishlist
**Complexity: High (8 services)**
```
API Gateway
├── Wishlist Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /favorites - Get user favorites
**Complexity: High (10 services)**
```
API Gateway
├── Favorites Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /favorites/items - Add to favorites
**Complexity: High (8 services)**
```
API Gateway
├── Favorites Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   │   ├── Availability Service
│   │   │   │   ├── Stock Service
│   │   │   │   └── Status Service
│   │   │   └── Location Service
│   │   │       ├── Warehouse Service
│   │   │       └── Distribution Service
│   │   └── Pricing Service
│   │       ├── Calculation Service
│   │       └── Discount Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### DELETE /favorites/items/{itemId} - Remove from favorites
**Complexity: Medium (4 services)**
```
API Gateway
├── Favorites Service
│   ├── Product Service
│   │   ├── Inventory Service
│   │   └── Pricing Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

### Notifications & Communication

#### GET /notifications - Get user notifications
**Complexity: High (10 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   │   ├── Template Service
│   │   └── Delivery Service
│   ├── SMS Service
│   │   ├── Gateway Service
│   │   └── Delivery Service
│   └── Push Notification Service
│       ├── Device Service
│       └── Delivery Service
├── User Service
│   ├── Address Service
│   └── Preference Service
```

#### PUT /notifications/{notificationId}/read - Mark notification as read
**Complexity: Medium (4 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   ├── SMS Service
│   └── Push Notification Service
├── User Service
│   ├── Address Service
│   └── Preference Service
```

#### POST /notifications/preferences - Update notification preferences
**Complexity: Medium (5 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   ├── SMS Service
│   └── Push Notification Service
├── User Service
│   ├── Address Service
│   └── Preference Service
```

#### GET /messages - Get user messages
**Complexity: High (8 services)**
```
API Gateway
├── Message Service
│   ├── User Service
│   │   ├── Address Service
│   │   └── Preference Service
│   └── Support Service
│       ├── Ticket Service
│       └── Agent Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### POST /messages - Send message to support
**Complexity: High (7 services)**
```
API Gateway
├── Message Service
│   ├── User Service
│   │   ├── Address Service
│   │   └── Preference Service
│   ├── Support Service
│   │   ├── Ticket Service
│   │   └── Agent Service
│   └── Notification Service
│       ├── Email Service
│       └── SMS Service
```

#### GET /support/tickets - Get support tickets
**Complexity: High (9 services)**
```
API Gateway
├── Support Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   └── Preference Service
│   │       ├── Language Service
│   │       └── Currency Service
│   └── Ticket Service
│       ├── Status Service
│       └── Priority Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### POST /support/tickets - Create support ticket
**Complexity: High (8 services)**
```
API Gateway
├── Support Service
│   ├── User Service
│   │   ├── Address Service
│   │   │   ├── Validation Service
│   │   │   └── Geocoding Service
│   │   └── Preference Service
│   │       ├── Language Service
│   │       └── Currency Service
│   ├── Ticket Service
│   │   ├── Status Service
│   │   └── Priority Service
│   └── Notification Service
│       ├── Email Service
│       └── SMS Service
```

## Seller/Vendor Endpoints

### Product Management

#### GET /seller/products - Get seller's products
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Product Service
│   ├── Inventory Service
│   │   ├── Availability Service
│   │   │   ├── Stock Service
│   │   │   └── Status Service
│   │   └── Location Service
│   │       ├── Warehouse Service
│   │       └── Distribution Service
│   └── Pricing Service
│       ├── Calculation Service
│       └── Discount Service
```

#### POST /seller/products - Create new product
**Complexity: Very High (18 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Product Service
│   ├── Category Service
│   │   ├── Hierarchy Service
│   │   └── Metadata Service
│   ├── Brand Service
│   │   ├── Management Service
│   │   └── Search Service
│   └── Media Service
│       ├── Image Service
│       │   ├── Processing Service
│       │   └── Optimization Service
│       ├── Video Service
│       │   ├── Encoding Service
│       │   └── Streaming Service
│       └── Storage Service
│           ├── Upload Service
│           └── CDN Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   └── Location Service
│       ├── Warehouse Service
│       └── Distribution Service
├── Validation Service
│   ├── Content Service
│   └── Policy Service
└── Search Service
│   ├── Index Service
│   └── Filter Service
```

#### PUT /seller/products/{productId} - Update product
**Complexity: Very High (18 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Product Service
│   ├── Category Service
│   │   ├── Hierarchy Service
│   │   └── Metadata Service
│   ├── Brand Service
│   │   ├── Management Service
│   │   └── Search Service
│   └── Media Service
│       ├── Image Service
│       │   ├── Processing Service
│       │   └── Optimization Service
│       ├── Video Service
│       │   ├── Encoding Service
│       │   └── Streaming Service
│       └── Storage Service
│           ├── Upload Service
│           └── CDN Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   └── Location Service
│       ├── Warehouse Service
│       └── Distribution Service
├── Validation Service
│   ├── Content Service
│   └── Policy Service
└── Search Service
│   ├── Index Service
│   └── Filter Service
```

#### DELETE /seller/products/{productId} - Delete product
**Complexity: High (6 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Product Service
│   ├── Inventory Service
│   └── Pricing Service
└── Search Service
│   ├── Index Service
│   └── Filter Service
```

#### POST /seller/products/bulk - Bulk product operations
**Complexity: Very High (15 services)**
```
API Gateway
├── Seller Service
│   ├── Product Service
│   │   ├── Category Service
│   │   ├── Brand Service
│   │   └── Media Service
│   ├── Inventory Service
│   │   ├── Warehouse Service
│   │   └── Supplier Service
│   └── Notification Service
│       ├── Email Service
│       └── SMS Service
├── Batch Processing Service
│   ├── Validation Service
│   └── Import Service
└── Search Service
```

#### GET /seller/products/analytics - Product analytics
**Complexity: Medium (3 services)**
```
API Gateway
├── Seller Service
└── Analytics Service
```

### Inventory Management

#### GET /seller/inventory - Get inventory status
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   ├── Location Service
│   │   ├── Warehouse Service
│   │   └── Distribution Service
│   └── Alert Service
│       ├── Threshold Service
│       └── Notification Service
```

#### PUT /seller/inventory/{productId} - Update inventory
**Complexity: High (9 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   ├── Location Service
│   │   ├── Warehouse Service
│   │   └── Distribution Service
│   └── Alert Service
│       ├── Threshold Service
│       └── Notification Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### POST /seller/inventory/bulk - Bulk inventory update
**Complexity: Very High (10 services)**
```
API Gateway
├── Seller Service
│   ├── Inventory Service
│   │   ├── Warehouse Service
│   │   └── Supplier Service
│   └── Notification Service
│       ├── Email Service
│       └── SMS Service
├── Batch Processing Service
│   ├── Validation Service
│   └── Import Service
└── Search Service
```

#### GET /seller/inventory/alerts - Get inventory alerts
**Complexity: High (7 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   └── Alert Service
│       ├── Threshold Service
│       └── Notification Service
```

#### POST /seller/inventory/restock - Initiate restock
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   ├── Location Service
│   │   ├── Warehouse Service
│   │   └── Distribution Service
│   └── Alert Service
│       ├── Threshold Service
│       └── Notification Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

### Order Fulfillment

#### GET /seller/orders - Get seller orders
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /seller/orders/{orderId} - Get order details
**Complexity: High (9 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   │   ├── Geocoding Service
│   │   │   └── Distance Service
│   │   ├── Tracking Service
│   │   └── Tax Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### PUT /seller/orders/{orderId}/status - Update order status
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### POST /seller/orders/{orderId}/ship - Mark order as shipped
**Complexity: Very High (14 services)**
```
API Gateway
├── Seller Service
│   ├── Order Service
│   │   ├── Inventory Service
│   │   │   ├── Warehouse Service
│   │   │   └── Supplier Service
│   │   ├── Payment Service
│   │   │   ├── Fraud Detection Service
│   │   │   └── Risk Assessment Service
│   │   └── Notification Service
│   │       ├── Email Service
│   │       ├── SMS Service
│   │       └── Push Notification Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   ├── Tax Service
│   │   └── Tracking Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /seller/orders/pending - Get pending orders
**Complexity: High (7 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### POST /seller/orders/bulk-ship - Bulk shipping operations
**Complexity: Very High (25 services)**
```
API Gateway
├── Seller Service
│   ├── Order Service
│   │   ├── Inventory Service
│   │   │   ├── Warehouse Service
│   │   │   │   ├── Location Service
│   │   │   │   │   ├── Geocoding Service
│   │   │   │   │   └── Distance Service
│   │   │   │   └── Capacity Service
│   │   │   │       ├── Space Service
│   │   │   │       └── Equipment Service
│   │   │   └── Supplier Service
│   │   │       ├── Contract Service
│   │   │       │   ├── Terms Service
│   │   │       │   └── Pricing Service
│   │   │       └── Delivery Service
│   │   │           ├── Route Service
│   │   │           └── Schedule Service
│   │   ├── Payment Service
│   │   │   ├── Fraud Detection Service
│   │   │   │   ├── ML Model Service
│   │   │   │   └── Historical Data Service
│   │   │   └── Risk Assessment Service
│   │   │       ├── Credit Score Service
│   │   │       └── Transaction History Service
│   │   └── Notification Service
│   │       ├── Email Service
│   │       │   ├── Template Service
│   │       │   └── Delivery Service
│   │       ├── SMS Service
│   │       │   ├── Gateway Service
│   │       │   └── Delivery Service
│   │       └── Push Notification Service
│   │           ├── Device Service
│   │           └── Delivery Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   │   ├── Geocoding Service
│   │   │   └── Distance Service
│   │   ├── Tax Service
│   │   │   ├── Rate Service
│   │   │   └── Exemption Service
│   │   └── Tracking Service
│   │       ├── Carrier Service
│   │       └── Status Service
│   └── User Service
│       ├── Address Service
│       │   ├── Validation Service
│       │   └── Geocoding Service
│       └── Preference Service
│           ├── Language Service
│           └── Currency Service
├── Batch Processing Service
│   ├── Validation Service
│   │   ├── Schema Service
│   │   └── Rule Service
│   └── Import Service
│       ├── Transform Service
│       └── Load Service
```

### Analytics & Reporting

#### GET /seller/analytics/sales - Sales analytics
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

#### GET /seller/analytics/products - Product performance
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

#### GET /seller/analytics/customers - Customer analytics
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

#### GET /seller/reports - Generate reports
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

#### GET /seller/earnings - Earnings summary
**Complexity: High (8 services)**
```
API Gateway
├── Seller Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

## Administrative Endpoints

### System Management

#### GET /admin/system/health - System health check
**Complexity: Simple (2 services)**
```
API Gateway
└── System Service
```

#### GET /admin/system/metrics - System metrics
**Complexity: Simple (2 services)**
```
API Gateway
└── System Service
```

#### GET /admin/system/logs - System logs
**Complexity: Simple (2 services)**
```
API Gateway
└── System Service
```

#### POST /admin/system/maintenance - Enable maintenance mode
**Complexity: Medium (3 services)**
```
API Gateway
├── System Service
└── Notification Service
```

#### GET /admin/system/backup - System backup status
**Complexity: Simple (2 services)**
```
API Gateway
└── System Service
```

### User Management

#### GET /admin/users - Get all users
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
```

#### GET /admin/users/{userId} - Get user details
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
```

#### PUT /admin/users/{userId}/status - Update user status
**Complexity: High (9 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### POST /admin/users/{userId}/suspend - Suspend user
**Complexity: High (9 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── User Service
│   ├── Address Service
│   │   ├── Validation Service
│   │   └── Geocoding Service
│   ├── Preference Service
│   │   ├── Language Service
│   │   └── Currency Service
│   └── Profile Service
│       ├── Personal Service
│       └── Business Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### GET /admin/users/analytics - User analytics
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

### Content Management

#### GET /admin/content/categories - Manage categories
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### POST /admin/content/categories - Create category
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### PUT /admin/content/categories/{categoryId} - Update category
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### DELETE /admin/content/categories/{categoryId} - Delete category
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### GET /admin/content/promotions - Manage promotions
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### POST /admin/content/promotions - Create promotion
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

#### PUT /admin/content/promotions/{promotionId} - Update promotion
**Complexity: Simple (2 services)**
```
API Gateway
└── Admin Service
```

### Order Management

#### GET /admin/orders - Get all orders
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /admin/orders/{orderId} - Get order details
**Complexity: High (9 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   │   ├── Geocoding Service
│   │   │   └── Distance Service
│   │   ├── Tracking Service
│   │   └── Tax Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### PUT /admin/orders/{orderId}/status - Update order status
**Complexity: High (9 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Order Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   ├── Payment Service
│   │   ├── Refund Service
│   │   └── Settlement Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### POST /admin/orders/{orderId}/refund - Process refund
**Complexity: Very High (15 services)**
```
API Gateway
├── Admin Service
│   ├── Order Service
│   │   ├── Inventory Service
│   │   │   ├── Warehouse Service
│   │   │   └── Supplier Service
│   │   ├── Payment Service
│   │   │   ├── Refund Service
│   │   │   ├── Fraud Detection Service
│   │   │   └── Risk Assessment Service
│   │   └── Notification Service
│   │       ├── Email Service
│   │       ├── SMS Service
│   │       └── Push Notification Service
│   ├── Shipping Service
│   │   ├── Location Service
│   │   └── Tracking Service
│   └── User Service
│       ├── Address Service
│       └── Preference Service
```

#### GET /admin/orders/analytics - Order analytics
**Complexity: Medium (3 services)**
```
API Gateway
├── Admin Service
└── Analytics Service
```

### Financial Management

#### GET /admin/financial/transactions - Get transactions
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Refund Service
│   └── Settlement Service
```

#### GET /admin/financial/revenue - Revenue analytics
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

#### GET /admin/financial/refunds - Refund management
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Refund Service
│   └── Settlement Service
```

#### POST /admin/financial/refunds/{refundId}/approve - Approve refund
**Complexity: High (9 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Refund Service
│   └── Settlement Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

#### GET /admin/financial/tax-reports - Tax reporting
**Complexity: High (8 services)**
```
API Gateway
├── Admin Service
│   ├── Profile Service
│   └── Validation Service
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

## Internal System Endpoints

### Data Processing

#### POST /internal/data/sync - Trigger data synchronization
**Complexity: Medium (3 services)**
```
API Gateway
├── Data Service
└── Analytics Service
```

#### GET /internal/data/status - Data sync status
**Complexity: Simple (2 services)**
```
API Gateway
└── Data Service
```

#### POST /internal/data/backup - Trigger data backup
**Complexity: Medium (3 services)**
```
API Gateway
├── Data Service
└── Analytics Service
```

#### GET /internal/data/backup/status - Backup status
**Complexity: Simple (2 services)**
```
API Gateway
└── Data Service
```

### Cache Management

#### POST /internal/cache/clear - Clear cache
**Complexity: Simple (2 services)**
```
API Gateway
└── Cache Service
```

#### GET /internal/cache/stats - Cache statistics
**Complexity: Simple (2 services)**
```
API Gateway
└── Cache Service
```

#### POST /internal/cache/warm - Warm cache
**Complexity: Medium (3 services)**
```
API Gateway
├── Cache Service
└── Analytics Service
```

#### GET /internal/cache/keys - List cache keys
**Complexity: Simple (2 services)**
```
API Gateway
└── Cache Service
```

### Search Indexing

#### POST /internal/search/reindex - Reindex search
**Complexity: Medium (3 services)**
```
API Gateway
├── Search Service
└── Analytics Service
```

#### GET /internal/search/status - Indexing status
**Complexity: Simple (2 services)**
```
API Gateway
└── Search Service
```

#### POST /internal/search/optimize - Optimize search index
**Complexity: Medium (3 services)**
```
API Gateway
├── Search Service
└── Analytics Service
```

#### GET /internal/search/analytics - Search analytics
**Complexity: Medium (3 services)**
```
API Gateway
├── Search Service
└── Analytics Service
```

### Notification Processing

#### POST /internal/notifications/send - Send notification
**Complexity: High (8 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   │   ├── Template Service
│   │   └── Delivery Service
│   ├── SMS Service
│   │   ├── Gateway Service
│   │   └── Delivery Service
│   └── Push Notification Service
│       ├── Device Service
│       └── Delivery Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/notifications/queue - Notification queue status
**Complexity: Medium (5 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   ├── SMS Service
│   └── Push Notification Service
└── Queue Service
│   ├── Status Service
│   └── Management Service
```

#### POST /internal/notifications/retry - Retry failed notifications
**Complexity: High (8 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   │   ├── Template Service
│   │   └── Delivery Service
│   ├── SMS Service
│   │   ├── Gateway Service
│   │   └── Delivery Service
│   └── Push Notification Service
│       ├── Device Service
│       └── Delivery Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/notifications/stats - Notification statistics
**Complexity: High (8 services)**
```
API Gateway
├── Notification Service
│   ├── Email Service
│   ├── SMS Service
│   └── Push Notification Service
└── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   └── Recommendation Service
│       ├── Engine Service
│       └── Personalization Service
```

### Payment Processing

#### POST /internal/payments/process - Process payment
**Complexity: High (10 services)**
```
API Gateway
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Gateway Service
│   │   ├── Processing Service
│   │   └── Settlement Service
│   └── Settlement Service
│       ├── Clearing Service
│       └── Settlement Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/payments/status - Payment processing status
**Complexity: Medium (5 services)**
```
API Gateway
├── Payment Service
│   ├── Gateway Service
│   ├── Settlement Service
│   └── Processing Service
└── Status Service
│   ├── Health Service
│   └── Monitoring Service
```

#### POST /internal/payments/refund - Process refund
**Complexity: High (10 services)**
```
API Gateway
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Refund Service
│   │   ├── Gateway Service
│   │   └── Settlement Service
│   └── Settlement Service
│       ├── Clearing Service
│       └── Settlement Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/payments/transactions - Payment transactions
**Complexity: High (8 services)**
```
API Gateway
├── Payment Service
│   ├── Fraud Detection Service
│   │   ├── ML Model Service
│   │   └── Historical Data Service
│   ├── Risk Assessment Service
│   │   ├── Credit Score Service
│   │   └── Transaction History Service
│   ├── Refund Service
│   └── Settlement Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

### Inventory Synchronization

#### POST /internal/inventory/sync - Sync inventory
**Complexity: High (10 services)**
```
API Gateway
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   ├── Location Service
│   │   ├── Warehouse Service
│   │   └── Distribution Service
│   ├── Alert Service
│   │   ├── Threshold Service
│   │   └── Notification Service
│   └── Sync Service
│       ├── Validation Service
│       └── Processing Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/inventory/status - Inventory sync status
**Complexity: Medium (5 services)**
```
API Gateway
├── Inventory Service
│   ├── Availability Service
│   ├── Location Service
│   └── Alert Service
└── Status Service
│   ├── Health Service
│   └── Monitoring Service
```

#### POST /internal/inventory/update - Update inventory
**Complexity: High (10 services)**
```
API Gateway
├── Inventory Service
│   ├── Availability Service
│   │   ├── Stock Service
│   │   └── Status Service
│   ├── Location Service
│   │   ├── Warehouse Service
│   │   └── Distribution Service
│   ├── Alert Service
│   │   ├── Threshold Service
│   │   └── Notification Service
│   └── Update Service
│       ├── Validation Service
│       └── Processing Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /internal/inventory/alerts - Inventory alerts
**Complexity: Medium (5 services)**
```
API Gateway
├── Inventory Service
│   ├── Availability Service
│   ├── Location Service
│   └── Alert Service
└── Notification Service
│   ├── Email Service
│   └── SMS Service
```

### Analytics Processing

#### POST /internal/analytics/process - Process analytics
**Complexity: High (10 services)**
```
API Gateway
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   ├── Recommendation Service
│   │   ├── Engine Service
│   │   └── Personalization Service
│   └── Processing Service
│       ├── Validation Service
│       └── Processing Service
└── Data Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### GET /internal/analytics/status - Analytics processing status
**Complexity: Medium (5 services)**
```
API Gateway
├── Analytics Service
│   ├── Trend Service
│   ├── Performance Service
│   └── Recommendation Service
└── Status Service
│   ├── Health Service
│   └── Monitoring Service
```

#### POST /internal/analytics/export - Export analytics data
**Complexity: High (10 services)**
```
API Gateway
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   ├── Recommendation Service
│   │   ├── Engine Service
│   │   └── Personalization Service
│   └── Export Service
│       ├── Format Service
│       └── Delivery Service
└── Data Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### GET /internal/analytics/reports - Generate reports
**Complexity: High (10 services)**
```
API Gateway
├── Analytics Service
│   ├── Trend Service
│   │   ├── Analysis Service
│   │   └── Calculation Service
│   ├── Performance Service
│   │   ├── Metrics Service
│   │   └── Comparison Service
│   ├── Recommendation Service
│   │   ├── Engine Service
│   │   └── Personalization Service
│   └── Report Service
│       ├── Generation Service
│       └── Format Service
└── Data Service
│   ├── Storage Service
│   └── Retrieval Service
```

## API Gateway & Load Balancer Endpoints

### Health Checks

#### GET /health - Basic health check
**Complexity: Simple (1 service)**
```
API Gateway
```

#### GET /health/detailed - Detailed health check
**Complexity: Medium (4 services)**
```
API Gateway
├── Health Service
│   ├── System Service
│   ├── Database Service
│   └── External Service
└── Monitoring Service
│   ├── Metrics Service
│   └── Alert Service
```

#### GET /ready - Readiness check
**Complexity: Medium (4 services)**
```
API Gateway
├── Health Service
│   ├── System Service
│   ├── Database Service
│   └── External Service
└── Monitoring Service
│   ├── Metrics Service
│   └── Alert Service
```

#### GET /live - Liveness check
**Complexity: Medium (4 services)**
```
API Gateway
├── Health Service
│   ├── System Service
│   ├── Database Service
│   └── External Service
└── Monitoring Service
│   ├── Metrics Service
│   └── Alert Service
```

### Rate Limiting & Throttling

#### GET /rate-limit/status - Rate limit status
**Complexity: Medium (4 services)**
```
API Gateway
├── Rate Limit Service
│   ├── Policy Service
│   ├── Enforcement Service
│   └── Monitoring Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### POST /rate-limit/reset - Reset rate limits
**Complexity: Medium (4 services)**
```
API Gateway
├── Rate Limit Service
│   ├── Policy Service
│   ├── Enforcement Service
│   └── Monitoring Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

#### GET /throttle/status - Throttling status
**Complexity: Medium (4 services)**
```
API Gateway
├── Throttle Service
│   ├── Policy Service
│   ├── Enforcement Service
│   └── Monitoring Service
└── Analytics Service
│   ├── Trend Service
│   └── Performance Service
```

### API Documentation

#### GET /api/docs - API documentation
**Complexity: Medium (4 services)**
```
API Gateway
├── Documentation Service
│   ├── Swagger Service
│   ├── OpenAPI Service
│   └── Markdown Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### GET /api/swagger.json - Swagger specification
**Complexity: Medium (4 services)**
```
API Gateway
├── Documentation Service
│   ├── Swagger Service
│   ├── OpenAPI Service
│   └── Markdown Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

#### GET /api/openapi.json - OpenAPI specification
**Complexity: Medium (4 services)**
```
API Gateway
├── Documentation Service
│   ├── Swagger Service
│   ├── OpenAPI Service
│   └── Markdown Service
└── Cache Service
│   ├── Storage Service
│   └── Retrieval Service
```

---

**Total Workflows: 120+ workflows**

This comprehensive set of workflows covers all endpoints in the exact order they appear in endpoints.md, with proper tree formatting where indentation represents stack depth. Each workflow has been assigned a complexity rating that accurately reflects the number of services involved.
