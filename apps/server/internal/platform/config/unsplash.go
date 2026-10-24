package config

// UnsplashConfig is the Unsplash API access the survey editor's image backgrounds search through, forma2's UNSPLASH_ACCESS_KEY. Without a key the editor offers no Unsplash search.
type UnsplashConfig struct {
	AccessKey Secret `env:"UNSPLASH_ACCESS_KEY"`
}

// Configured is whether the editor offers the Unsplash image tab.
func (u UnsplashConfig) Configured() bool { return u.AccessKey != "" }
