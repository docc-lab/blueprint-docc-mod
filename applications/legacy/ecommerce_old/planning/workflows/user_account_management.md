# Ecommerce Marketplace Workflows - User Account Management

This document defines the distributed workflows for user account management endpoints in the ecommerce marketplace, focusing on deep call chains that make logical sense from first principles.

## User Account Management

### POST /auth/register - User registration
```
[1] AuthService
├── [2] ValidationService
│   ├── [3] EmailService
│   │   └── [4] VerificationService
│   ├── [3] PasswordService
│   │   └── [4] StrengthService
│   │       └── [5] PolicyService
│   └── [3] ProfileService
│       └── [4] SanitizationService
├── [2] UserService
│   ├── [3] ProfileService
│   │   └── [4] MetadataService
│   └── [3] PermissionService
│       └── [4] RoleService
└── [2] NotificationService
    └── [3] EmailService
        └── [4] TemplateService
```

### POST /auth/login - User login
```
[1] AuthService
├── [2] AuthenticationService
│   ├── [3] CredentialService
│   │   └── [4] VerificationService
│   │       └── [5] HashService
│   └── [3] SessionService
│       └── [4] TokenService
│           └── [5] GenerationService
└── [2] UserService
    └── [3] ProfileService
```

### POST /auth/logout - User logout
```
[1] AuthService
└── [2] SessionService
    └── [3] TokenService
        └── [4] InvalidationService
```

### GET /user/profile - Get user profile
```
[1] UserService
├── [2] ProfileService
│   └── [3] MetadataService
│       └── [4] AttributeService
│           └── [5] TagService
└── [2] PermissionService
    └── [3] RoleService
        └── [4] ValidationService
```

### PUT /user/profile - Update user profile
```
[1] UserService
├── [2] ProfileService
│   ├── [3] ValidationService
│   │   ├── [4] SanitizationService
│   │   └── [4] EmailService
│   │       └── [5] VerificationService

└── [2] NotificationService
    └── [3] EmailService
        └── [4] TemplateService
```

### GET /user/addresses - Get user addresses
```
[1] UserService
└── [2] AddressService
```

### POST /user/addresses - Add new address
```
[1] UserService
├── [2] AddressService
│   ├── [3] ValidationService
│   │   └── [4] GeocodingService
│   │       └── [5] LocationService
└── [2] NotificationService
    └── [3] EmailService
        └── [4] TemplateService
```

### PUT /user/addresses/{addressId} - Update address
```
[1] UserService
├── [2] AddressService
│   ├── [3] ValidationService
│   │   └── [4] GeocodingService
│   │       └── [5] LocationService
└── [2] NotificationService
    └── [3] EmailService
        └── [4] TemplateService
```

### DELETE /user/addresses/{addressId} - Delete address
```
[1] UserService
└── [2] AddressService
    └── [3] ValidationService
        └── [4] PermissionService
            └── [5] RoleService
```

### GET /user/preferences - Get user preferences
```
[1] UserService
└── [2] PreferenceService
```

### PUT /user/preferences - Update user preferences
```
[1] UserService
├── [2] PreferenceService
│   ├── [3] ValidationService
│   │   └── [4] CategoryService
│   │       └── [5] OrganizationService

└── [2] NotificationService
    └── [3] EmailService
        └── [4] TemplateService
```
