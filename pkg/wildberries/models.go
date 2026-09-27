package wildberries

import "time"

// CommissionItem contains a product category's Wildberries commission rates.
type CommissionItem struct {
	ParentID            int64   `json:"parentID"`
	ParentName          string  `json:"parentName"`
	SubjectID           int64   `json:"subjectID"`
	SubjectName         string  `json:"subjectName"`
	KgvpBooking         float64 `json:"kgvpBooking"`
	KgvpMarketplace     float64 `json:"kgvpMarketplace"`
	KgvpPickup          float64 `json:"kgvpPickup"`
	KgvpSupplier        float64 `json:"kgvpSupplier"`
	KgvpSupplierExpress float64 `json:"kgvpSupplierExpress"`
	PaidStorageKgvp     float64 `json:"paidStorageKgvp"`
}

// Product describes a product and its prices returned by the prices API.
type Product struct {
	NmID                    int64                        `json:"nmID"`
	VendorCode              string                       `json:"vendorCode"`
	SubjectID               int64                        `json:"subjectID"`
	Sizes                   []ProductSize                `json:"sizes"`
	CurrencyIsoCode4217     string                       `json:"currencyIsoCode4217"`
	Discount                int                          `json:"discount"`
	ClubDiscount            int                          `json:"clubDiscount"`
	EditableSizePrice       bool                         `json:"editableSizePrice"`
	WholesaleDiscountLevels []WholesaleDiscountThreshold `json:"wholesaleDiscountThreshold"`
}

// WholesaleDiscountThreshold describes one B2B wholesale discount level.
type WholesaleDiscountThreshold struct {
	MinQuantity       int `json:"minQuantity"`
	WholesaleDiscount int `json:"wholesaleDiscount"`
	Level             int `json:"level"`
}

// ProductSize contains the current price of one size of a product.
type ProductSize struct {
	SizeID              int64   `json:"sizeID"`
	Price               int64   `json:"price"`
	DiscountedPrice     float64 `json:"discountedPrice"`
	ClubDiscountedPrice float64 `json:"clubDiscountedPrice"`
	TechSizeName        string  `json:"techSizeName"`
}

// UploadTask contains status and timing metadata for a processed upload.
type UploadTask struct {
	UploadID           int64      `json:"uploadID"`
	Status             int        `json:"status"`
	UploadDate         time.Time  `json:"uploadDate"`
	ActivationDate     *time.Time `json:"activationDate"`
	OverallGoodsNumber int        `json:"overAllGoodsNumber"`
	SuccessGoodsNumber int        `json:"successGoodsNumber"`
}

// UploadTaskProduct is one product/size entry in a processed upload.
type UploadTaskProduct struct {
	NmID                int64   `json:"nmID"`
	VendorCode          string  `json:"vendorCode"`
	SizeID              *int64  `json:"sizeID"`
	TechSizeName        string  `json:"techSizeName"`
	Price               *int64  `json:"price"`
	CurrencyIsoCode4217 string  `json:"currencyIsoCode4217"`
	Discount            int     `json:"discount"`
	ClubDiscount        *int    `json:"clubDiscount"`
	Status              int     `json:"status"`
	ErrorText           *string `json:"errorText"`
}

// PricePoint describes the price and discount applied by a processed API upload.
type PricePoint struct {
	NmID                int64     `json:"nmID"`
	UploadID            int64     `json:"uploadID"`
	Timestamp           time.Time `json:"timestamp"`
	SizeID              *int64    `json:"sizeID,omitempty"`
	TechSizeName        string    `json:"techSizeName,omitempty"`
	Price               int64     `json:"price"`
	CurrencyIsoCode4217 string    `json:"currencyIsoCode4217,omitempty"`
	Discount            int       `json:"discount"`
	ClubDiscount        *int      `json:"clubDiscount,omitempty"`
}

type commissionsResponse struct {
	Report []CommissionItem `json:"report"`
}

type productsResponse struct {
	Data struct {
		ListGoods []Product `json:"listGoods"`
	} `json:"data"`
	Error     bool   `json:"error"`
	ErrorText string `json:"errorText"`
}

type uploadTaskResponse struct {
	Data      *UploadTask `json:"data"`
	Error     bool        `json:"error"`
	ErrorText string      `json:"errorText"`
}

type uploadTaskDetailsResponse struct {
	Data *struct {
		UploadID     *int64              `json:"uploadID"`
		HistoryGoods []UploadTaskProduct `json:"historyGoods"`
	} `json:"data"`
	Error     bool   `json:"error"`
	ErrorText string `json:"errorText"`
}
