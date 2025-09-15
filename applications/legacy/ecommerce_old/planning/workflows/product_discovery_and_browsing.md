# Ecommerce Marketplace Workflows

This document defines the distributed workflows for each endpoint in the ecommerce marketplace, focusing on deep call chains that make logical sense from first principles.

## User-Facing Endpoints

### Product Discovery & Browsing

#### GET /products - Browse products with filtering and pagination
```
[1] ProductService
├── [2] CatalogService
│   ├── [3] CategoryService
│   │   └── [4] HierarchyService
│   │       └── [5] NavigationService
│   ├── [3] InventoryService
│   │   └── [4] StockService
│   └── [3] PricingService
│       └── [4] DiscountService
└── [2] FilterService
    └── [3] PaginationService
```

#### GET /products/{productId} - Get detailed product information
```
[1] ProductService
├── [2] CatalogService
│   ├── [3] CategoryService
│   │   └── [4] HierarchyService
│   │       └── [5] NavigationService
│   ├── [3] InventoryService
│   │   └── [4] StockService
│   └── [3] PricingService
│       └── [4] DiscountService
├── [2] ReviewService
│   └── [3] RatingService
│       └── [4] CalculationService
│           └── [5] AggregationService
│               └── [6] SummaryService
└── [2] MediaService
    └── [3] ImageService
        └── [4] ProcessingService
            └── [5] OptimizationService
```

#### GET /products/search - Search products with advanced filters
```
[1] ProductService
├── [2] SearchService
│   └── [3] QueryService
│       └── [4] ParserService
│           └── [5] TokenizerService
│               └── [6] FilterService
│                   └── [7] ValidatorService
│                       └── [8] BuilderService
├── [2] CatalogService
│   ├── [3] CategoryService
│   │   └── [4] HierarchyService
│   │       └── [5] NavigationService
│   ├── [3] InventoryService
│   │   └── [4] StockService
│   └── [3] PricingService
│       └── [4] DiscountService
└── [2] SortService
    └── [3] OrderService
        └── [4] PaginationService
```

#### GET /categories - Browse product categories
```
[1] CategoryService
├── [2] HierarchyService
│   └── [3] NavigationService
│       └── [4] MetadataService
│           └── [5] AttributeService
│               └── [6] TagService
└── [2] ProductService
    └── [3] CatalogService
        ├── [4] InventoryService
        │   └── [5] StockService
        └── [4] PricingService
            └── [5] DiscountService
```

#### GET /categories/{categoryId}/products - Products by category
```
[1] CategoryService
├── [2] HierarchyService
│   └── [3] NavigationService
│       └── [4] MetadataService
│           └── [5] AttributeService
│               └── [6] TagService
└── [2] ProductService
    ├── [3] CatalogService
    │   ├── [4] InventoryService
    │   │   └── [5] StockService
    │   └── [4] PricingService
    │       └── [5] DiscountService
    └── [3] FilterService
        └── [4] PaginationService
```

#### GET /deals - Get current deals and promotions
```
[1] PromotionService
├── [2] DiscountService
│   └── [3] CouponService
│       └── [4] ProductService
│           ├── [5] CatalogService
│           │   ├── [6] InventoryService
│           │   │   └── [7] StockService
│           │   └── [6] PricingService
│           │       └── [7] DiscountService
│           └── [5] FilterService
│               └── [6] PaginationService
└── [2] CampaignService
    └── [3] ManagementService
        └── [4] SchedulingService
            └── [5] ValidationService
                └── [6] EligibilityService
                    └── [7] LimitService
                        └── [8] ApplicationService
```

#### GET /brands - Browse brands
```
[1] BrandService
├── [2] ManagementService
│   └── [3] ProfileService
│       └── [4] MetadataService
│           └── [5] AttributeService
│               └── [6] TagService
└── [2] ProductService
    └── [3] CatalogService
        ├── [4] InventoryService
        │   └── [5] StockService
        └── [4] PricingService
            └── [5] DiscountService
```

#### GET /brands/{brandId}/products - Products by brand
```
[1] BrandService
├── [2] ManagementService
│   └── [3] ProfileService
│       └── [4] MetadataService
│           └── [5] AttributeService
│               └── [6] TagService
└── [2] ProductService
    ├── [3] CatalogService
    │   ├── [4] InventoryService
    │   │   └── [5] StockService
    │   └── [4] PricingService
    │       └── [5] DiscountService
    └── [3] FilterService
        └── [4] PaginationService
```
