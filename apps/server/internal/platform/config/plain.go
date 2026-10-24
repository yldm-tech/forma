package config

// PlainConfig is forma2's Plain in-product support chat (PLAIN_*), on Forma Cloud. The chat is shown only when PLAIN_APP_ID is set.
type PlainConfig struct {
	// AppID is the chat widget's app id from Plain's Chat settings.
	AppID string `env:"PLAIN_APP_ID"`
	// ChatHMACSecret signs the signed-in user's email so Plain can verify the customer's identity; without it the chat runs unverified.
	ChatHMACSecret Secret `env:"PLAIN_CHAT_HMAC_SECRET"`
	// ActiveCustomerLabelTypeID is the Plain label type applied to the threads of users in an organization on a paid, active plan.
	ActiveCustomerLabelTypeID string `env:"PLAIN_ACTIVE_CUSTOMER_LABEL_TYPE_ID"`
}

// Configured is forma2's IS_PLAIN_CHAT_CONFIGURED.
func (p PlainConfig) Configured() bool {
	return p.AppID != ""
}
