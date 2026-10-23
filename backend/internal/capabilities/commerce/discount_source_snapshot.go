package commerce

// DiscountSourceSnapshot is host evidence from a strictly verified paid series.
// Browser input can select neither its source IDs nor the prior agreement.
type DiscountSourceSnapshot struct {
	Version               int               `json:"version"`
	SubscriptionID        uint              `json:"subscription_id"`
	RootOrderID           uint              `json:"root_order_id"`
	BaseOrderID           uint              `json:"base_order_id"`
	AgreementOrderID      uint              `json:"agreement_order_id"`
	Fingerprint           string            `json:"fingerprint"`
	EntitlementEpoch      uint64            `json:"entitlement_epoch"`
	ProviderID            string            `json:"provider_id"`
	SigningKeyFingerprint string            `json:"signing_key_fingerprint"`
	Agreement             DiscountAgreement `json:"agreement"`
	Selection             string            `json:"selection,omitempty"`
}

func ValidateDiscountSource(source *DiscountSourceSnapshot) error {
	if source == nil || source.Version != 1 || source.SubscriptionID == 0 || source.RootOrderID == 0 || source.BaseOrderID != source.RootOrderID || source.AgreementOrderID < source.RootOrderID ||
		source.ProviderID == "" || len(source.ProviderID) > 160 || !discountHashPattern.MatchString(source.SigningKeyFingerprint) || !discountHashPattern.MatchString(source.Fingerprint) || len(source.Selection) > 256 ||
		source.Agreement.Scope != "recurring" || validateDiscountAgreement(&source.Agreement) != nil {
		return ErrDiscountQuoteInvalid
	}
	return nil
}
