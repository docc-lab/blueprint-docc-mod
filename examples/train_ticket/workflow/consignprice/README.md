<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# consignprice

```go
import "gitlab.mpi-sws.org/cld/blueprint/examples/train_ticket/workflow/consignprice"
```

Package consignprice implements ts\-consignprice\-service from the original train ticket application

## Index

- [type ConsignPrice](<#ConsignPrice>)
- [type ConsignPriceService](<#ConsignPriceService>)
- [type ConsignPriceServiceImpl](<#ConsignPriceServiceImpl>)
  - [func NewConsignPriceServiceImpl\(ctx context.Context, db backend.NoSQLDatabase\) \(\*ConsignPriceServiceImpl, error\)](<#NewConsignPriceServiceImpl>)
  - [func \(c \*ConsignPriceServiceImpl\) CreateAndModifyPriceConfig\(ctx context.Context, priceConfig ConsignPrice\) \(ConsignPrice, error\)](<#ConsignPriceServiceImpl.CreateAndModifyPriceConfig>)
  - [func \(c \*ConsignPriceServiceImpl\) GetPriceByWeightAndRegion\(ctx context.Context, weight float64, isWithinRegion bool\) \(float64, error\)](<#ConsignPriceServiceImpl.GetPriceByWeightAndRegion>)
  - [func \(c \*ConsignPriceServiceImpl\) GetPriceConfig\(ctx context.Context\) \(ConsignPrice, error\)](<#ConsignPriceServiceImpl.GetPriceConfig>)
  - [func \(c \*ConsignPriceServiceImpl\) GetPriceInfo\(ctx context.Context\) \(string, error\)](<#ConsignPriceServiceImpl.GetPriceInfo>)


<a name="ConsignPrice"></a>
## type [ConsignPrice](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/data.go#L3-L10>)



```go
type ConsignPrice struct {
    ID            string
    Index         int64
    InitialWeight float64
    InitialPrice  float64
    WithinPrice   float64
    BeyondPrice   float64
}
```

<a name="ConsignPriceService"></a>
## type [ConsignPriceService](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L14-L23>)

ConsignPriceService manages the prices of consignments

```go
type ConsignPriceService interface {
    // Calculates the price of the consignment based on the weight and the region
    GetPriceByWeightAndRegion(ctx context.Context, weight float64, isWithinRegion bool) (float64, error)
    // Get the price configuration for calculating consignment prices as a string
    GetPriceInfo(ctx context.Context) (string, error)
    // Get the price configuration for calculating consignment prices
    GetPriceConfig(ctx context.Context) (ConsignPrice, error)
    // Creates a price config or modifies the existing price configuration
    CreateAndModifyPriceConfig(ctx context.Context, priceConfig ConsignPrice) (ConsignPrice, error)
}
```

<a name="ConsignPriceServiceImpl"></a>
## type [ConsignPriceServiceImpl](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L25-L27>)



```go
type ConsignPriceServiceImpl struct {
    // contains filtered or unexported fields
}
```

<a name="NewConsignPriceServiceImpl"></a>
### func [NewConsignPriceServiceImpl](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L29>)

```go
func NewConsignPriceServiceImpl(ctx context.Context, db backend.NoSQLDatabase) (*ConsignPriceServiceImpl, error)
```



<a name="ConsignPriceServiceImpl.CreateAndModifyPriceConfig"></a>
### func \(\*ConsignPriceServiceImpl\) [CreateAndModifyPriceConfig](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L93>)

```go
func (c *ConsignPriceServiceImpl) CreateAndModifyPriceConfig(ctx context.Context, priceConfig ConsignPrice) (ConsignPrice, error)
```



<a name="ConsignPriceServiceImpl.GetPriceByWeightAndRegion"></a>
### func \(\*ConsignPriceServiceImpl\) [GetPriceByWeightAndRegion](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L33>)

```go
func (c *ConsignPriceServiceImpl) GetPriceByWeightAndRegion(ctx context.Context, weight float64, isWithinRegion bool) (float64, error)
```



<a name="ConsignPriceServiceImpl.GetPriceConfig"></a>
### func \(\*ConsignPriceServiceImpl\) [GetPriceConfig](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L72>)

```go
func (c *ConsignPriceServiceImpl) GetPriceConfig(ctx context.Context) (ConsignPrice, error)
```



<a name="ConsignPriceServiceImpl.GetPriceInfo"></a>
### func \(\*ConsignPriceServiceImpl\) [GetPriceInfo](<https://gitlab.mpi-sws.org/cld/blueprint2/blueprint/blob/main/examples/train_ticket/workflow/consignprice/consignPriceService.go#L50>)

```go
func (c *ConsignPriceServiceImpl) GetPriceInfo(ctx context.Context) (string, error)
```



Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)