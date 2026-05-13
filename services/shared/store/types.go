package store

import "time"

type SessionView struct {
	SessionID string    `json:"sessionId"`
	UserID    string    `json:"userId,omitempty"`
	AdminID   string    `json:"adminId,omitempty"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	Role      string    `json:"role,omitempty"`
	CSRFToken string    `json:"csrfToken,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone,omitempty"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AdminUser struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Plan struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Tier        string   `json:"tier"`
	PriceCents  int      `json:"priceCents"`
	Interval    string   `json:"interval"`
	Description string   `json:"description"`
	Features    []string `json:"features"`
}

type Subscription struct {
	ID        string     `json:"id"`
	PlanCode  string     `json:"planCode"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndsAt    *time.Time `json:"endsAt,omitempty"`
	AutoRenew bool       `json:"autoRenew"`
}

type APIKey struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Prefix             string     `json:"prefix"`
	Scopes             []string   `json:"scopes"`
	Status             string     `json:"status"`
	RateLimitPerMinute int        `json:"rateLimitPerMinute"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	RevealedKey        string     `json:"revealedKey,omitempty"`
}

type Conversation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	ModelSlug  string    `json:"modelSlug"`
	DeepSearch bool      `json:"deepSearch"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type ConversationShare struct {
	ID                   string    `json:"id"`
	ConversationID       string    `json:"conversationId"`
	UserID               string    `json:"userId,omitempty"`
	IsActive             bool      `json:"isActive"`
	RequireAccessCode    bool      `json:"requireAccessCode"`
	AccessCode           string    `json:"accessCode,omitempty"`
	CollaborationEnabled bool      `json:"collaborationEnabled"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type Message struct {
	ID               string            `json:"id"`
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoningContent,omitempty"`
	Attachments      []MessageAsset    `json:"attachments"`
	Sources          []SearchSource    `json:"sources,omitempty"`
	Artifacts        []MessageArtifact `json:"artifacts,omitempty"`
	ModelSlug        string            `json:"modelSlug"`
	CreatedAt        time.Time         `json:"createdAt"`
}

type MessageAsset struct {
	ID       string `json:"id"`
	FileName string `json:"fileName"`
	MimeType string `json:"mimeType"`
	URL      string `json:"url,omitempty"`
}

type SearchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Index   int    `json:"index,omitempty"`
}

type MessageArtifact struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	EntryFile string `json:"entryFile"`
	Language  string `json:"language,omitempty"`
}

type ArtifactFile struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Content  string `json:"content"`
}

type ChatArtifact struct {
	ID             string         `json:"id"`
	UserID         string         `json:"userId,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	MessageID      string         `json:"messageId,omitempty"`
	Title          string         `json:"title"`
	Kind           string         `json:"kind"`
	EntryFile      string         `json:"entryFile"`
	Files          []ArtifactFile `json:"files"`
	Version        int            `json:"version"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type ChatArtifactVersion struct {
	ID        string         `json:"id"`
	Version   int            `json:"version"`
	Files     []ArtifactFile `json:"files,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type ChatRun struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"userId,omitempty"`
	ConversationID     string     `json:"conversationId"`
	Status             string     `json:"status"`
	ModelSlug          string     `json:"modelSlug"`
	DeepSearch         bool       `json:"deepSearch"`
	UserMessageID      string     `json:"userMessageId,omitempty"`
	AssistantMessageID string     `json:"assistantMessageId,omitempty"`
	ErrorMessage       string     `json:"errorMessage,omitempty"`
	CancelRequested    bool       `json:"cancelRequested"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
}

type ChatRunEvent struct {
	Seq       int64          `json:"seq"`
	RunID     string         `json:"runId"`
	EventType string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

type AttachmentRecord struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	ConversationID *string   `json:"conversationId,omitempty"`
	ObjectKey      string    `json:"objectKey"`
	Bucket         string    `json:"bucket"`
	FileName       string    `json:"fileName"`
	MimeType       string    `json:"mimeType"`
	SizeBytes      int64     `json:"sizeBytes"`
	ExtractedText  string    `json:"extractedText,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ModelRoute struct {
	ID            string          `json:"id"`
	Slug          string          `json:"slug"`
	Name          string          `json:"name"`
	Protocol      string          `json:"protocol"`
	Strategy      string          `json:"strategy"`
	ModelType     string          `json:"modelType"`
	UpstreamModel string          `json:"upstreamModel"`
	Description   string          `json:"description"`
	SortOrder     int             `json:"sortOrder"`
	PromptEnabled bool            `json:"promptEnabled"`
	PromptText    string          `json:"promptText,omitempty"`
	Active        bool            `json:"active"`
	Endpoints     []ModelEndpoint `json:"endpoints"`
}

type ModelEndpoint struct {
	ID        string `json:"id"`
	SortOrder int    `json:"sortOrder"`
	BaseURL   string `json:"baseUrl"`
	Secret    string `json:"secret,omitempty"`
	Active    bool   `json:"active"`
}

type OAuthProvider struct {
	ID             string            `json:"id"`
	Slug           string            `json:"slug"`
	Name           string            `json:"name"`
	ProviderKind   string            `json:"providerKind"`
	Enabled        bool              `json:"enabled"`
	LogoURL        string            `json:"logoUrl"`
	AuthURL        string            `json:"authUrl"`
	TokenURL       string            `json:"tokenUrl"`
	UserInfoURL    string            `json:"userInfoUrl"`
	Scopes         string            `json:"scopes"`
	ClientID       string            `json:"clientId,omitempty"`
	ClientSecret   string            `json:"clientSecret,omitempty"`
	UserIDField    string            `json:"userIdField"`
	UserEmailField string            `json:"userEmailField"`
	UserNameField  string            `json:"userNameField"`
	AuthParams     map[string]string `json:"authParams,omitempty"`
	TokenParams    map[string]string `json:"tokenParams,omitempty"`
}

type AuthSecuritySettings struct {
	CaptchaRequiredOnRegister           bool `json:"captchaRequiredOnRegister"`
	PhoneVerificationRequiredOnRegister bool `json:"phoneVerificationRequiredOnRegister"`
	PhoneLoginEnabled                   bool `json:"phoneLoginEnabled"`
	SMSCodeTTLSeconds                   int  `json:"smsCodeTTLSeconds"`
	VerificationTestMode                bool `json:"verificationTestMode"`
	SMSGatewayConfigured                bool `json:"smsGatewayConfigured"`
	EmailGatewayConfigured              bool `json:"emailGatewayConfigured"`
}

type SMSGatewayConfig struct {
	Enabled         bool   `json:"enabled"`
	ProviderName    string `json:"providerName"`
	EndpointURL     string `json:"endpointUrl"`
	AuthScheme      string `json:"authScheme"`
	HeaderName      string `json:"headerName"`
	AuthToken       string `json:"authToken"`
	SenderID        string `json:"senderId"`
	MessageTemplate string `json:"messageTemplate"`
}

type EmailGatewayConfig struct {
	Enabled         bool   `json:"enabled"`
	ProviderName    string `json:"providerName"`
	EndpointURL     string `json:"endpointUrl"`
	AuthScheme      string `json:"authScheme"`
	HeaderName      string `json:"headerName"`
	AuthToken       string `json:"authToken"`
	FromAddress     string `json:"fromAddress"`
	FromName        string `json:"fromName"`
	SubjectTemplate string `json:"subjectTemplate"`
	ContentTemplate string `json:"contentTemplate"`
}

type RedeemPreview struct {
	Code         string     `json:"code"`
	PlanCode     string     `json:"planCode"`
	PlanName     string     `json:"planName"`
	DurationText string     `json:"durationText"`
	AccountType  string     `json:"accountType"`
	MaxUses      int        `json:"maxUses"`
	Remaining    int        `json:"remainingUses"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Status       string     `json:"status"`
}

type DownloadRelease struct {
	ID          string    `json:"id"`
	Platform    string    `json:"platform"`
	Channel     string    `json:"channel"`
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	DownloadURL string    `json:"downloadUrl"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type InfiniteCodeQuotaPlan struct {
	Credits    int `json:"credits"`
	ResetHours int `json:"resetHours"`
}

type ShareCollaborationPlan struct {
	MaxCollaborators int `json:"maxCollaborators"`
}

type ModelContextLimits struct {
	Default int                       `json:"default"`
	Models  map[string]int            `json:"models"`
	Plans   map[string]map[string]int `json:"plans"`
	Users   map[string]map[string]int `json:"users"`
}

type SearchProviderConfig struct {
	Enabled        bool   `json:"enabled"`
	Provider       string `json:"provider"`
	BaseURL        string `json:"baseUrl"`
	ResultCount    int    `json:"resultCount"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}
