# Workflow: product_search

**Endpoint:** `POST /products/search`
**Description:** Product search with advanced filtering
**Business Purpose:** Search products by criteria

## Call Tree

```
└─ [0] Product Catalog Service.search_products (depth: 1)
   ├─ [0] Authentication Service.validate_token (depth: 2)
   ├─ [1] Search Service.process_search_request (depth: 2) (depends on: [0])
   │  └─ [0] Search Query Service.validate_query (depth: 3)
   │     └─ [0] Search Index Service.prepare_query (depth: 4)
   │        └─ [0] Search Cache Service.check_cache (depth: 5)
   ├─ [2] Product Enrichment Service.enrich_search_results (depth: 2) (depends on: [1])
   │  └─ [0] Product Validation Service.validate_products (depth: 3)
   │     └─ [0] Product Cache Service.get_cached_products (depth: 4)
   │        └─ [0] Product Enrichment Service.enrich_product_details (depth: 5)
   ├─ [3] User Profile Service.get_user_preferences (depth: 2) (depends on: [2])
   │  └─ [0] User Enrichment Service.enrich_user_context (depth: 3)
   │     └─ [0] User Cache Service.get_cached_user_data (depth: 4)
   │        └─ [0] User Enrichment Service.process_user_context (depth: 5)
   └─ [4] Search Cache Service.cache_results (depth: 2) (depends on: [3])
```
