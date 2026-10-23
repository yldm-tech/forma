// Package unsplash is the survey editor's Unsplash image search for backgrounds, forma2's getImagesFromUnsplashAction and triggerDownloadUnsplashImageAction (apps/web/modules/survey/editor/actions.ts). Both spend the instance's UNSPLASH_ACCESS_KEY quota, so they need a session and are limited per user.
package unsplash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
)

// apiURL is Unsplash's API. A download location must be on its host, forma2's UNSPLASH_ALLOWED_DOMAINS.
const apiURL = "https://api.unsplash.com"

// perPage is forma2's page size: three rows of the editor's three-column grid.
const perPage = 9

// requestTimeout bounds one call to Unsplash.
const requestTimeout = 10 * time.Second

// maxResponseBytes bounds what is read of an Unsplash answer; a page of nine photos is a few tens of kilobytes.
const maxResponseBytes = 4 << 20

// MessageInvalidURL is forma2's refusal of a download location outside Unsplash's API.
const MessageInvalidURL = "Invalid Unsplash URL"

// errNotConfigured answers the search and the download ping on an instance without UNSPLASH_ACCESS_KEY, where the editor offers no Unsplash tab.
var errNotConfigured = &apperr.Error{Status: http.StatusNotFound, Code: apperr.CodeNotFound, Message: "Unsplash is not configured"}

// Image is one search result.
type Image struct {
	ID             string `json:"id"`
	AltDescription string `json:"altDescription"`
	// URL is the photo with forma2's attribution parameters (authorLink, authorName, utm_*), the value a survey stores as its background; the survey page reads the author back from it.
	URL string `json:"url"`
	// DownloadLocation is what the editor sends back to /download when the photo is chosen, as Unsplash's API guidelines require.
	DownloadLocation string `json:"downloadLocation"`
	AuthorName       string `json:"authorName"`
}

// Service calls Unsplash's API with the instance's access key.
type Service struct {
	accessKey config.Secret
	base      *url.URL
	client    *http.Client
	limiter   *ratelimit.Limiter
}

func NewService(cfg config.Config, limiter *ratelimit.Limiter) *Service {
	base, _ := url.Parse(apiURL)
	return &Service{accessKey: cfg.Unsplash.AccessKey, base: base, client: &http.Client{Timeout: requestTimeout}, limiter: limiter}
}

// Configured is forma2's isUnsplashConfigured: whether the editor offers the Unsplash tab.
func (s *Service) Configured() bool { return s.accessKey != "" }

// begin refuses a call on an instance without an access key and charges the caller's per-user limit.
func (s *Service) begin(ctx context.Context, userID uuid.UUID) error {
	if !s.Configured() {
		return errNotConfigured
	}
	return s.limiter.Apply(ctx, ratelimit.ActionUnsplash, userID.String())
}

// searchResult is the part of a /search/photos result the editor uses.
type searchResult struct {
	ID             string  `json:"id"`
	AltDescription *string `json:"alt_description"`
	User           struct {
		FirstName *string `json:"first_name"`
		LastName  *string `json:"last_name"`
		Links     struct {
			HTML string `json:"html"`
		} `json:"links"`
	} `json:"user"`
	URLs struct {
		Regular string `json:"regular"`
	} `json:"urls"`
	Links struct {
		DownloadLocation string `json:"download_location"`
	} `json:"links"`
}

// Search returns one page of landscape photos matching query.
func (s *Service) Search(ctx context.Context, userID uuid.UUID, query string, page int) ([]Image, error) {
	if err := s.begin(ctx, userID); err != nil {
		return nil, err
	}
	u := s.base.JoinPath("search", "photos")
	u.RawQuery = url.Values{
		"query":       {query},
		"orientation": {"landscape"},
		"per_page":    {strconv.Itoa(perPage)},
		"page":        {strconv.Itoa(page)},
	}.Encode()
	var answer struct {
		Results []searchResult `json:"results"`
	}
	if err := s.get(ctx, u.String(), &answer); err != nil {
		return nil, fmt.Errorf("search unsplash: %w", err)
	}
	images := make([]Image, 0, len(answer.Results))
	for _, r := range answer.Results {
		author := strings.TrimSpace(deref(r.User.FirstName) + " " + deref(r.User.LastName))
		images = append(images, Image{
			ID:             r.ID,
			AltDescription: deref(r.AltDescription),
			// forma2's regularWithAttribution. Unsplash's regular URL always carries a query, so the parameters are appended with &.
			URL:              r.URLs.Regular + "&dpr=2&authorLink=" + encodeComponent(r.User.Links.HTML) + "&authorName=" + encodeComponent(author) + "&utm_source=forma&utm_medium=referral",
			DownloadLocation: r.Links.DownloadLocation,
			AuthorName:       author,
		})
	}
	return images, nil
}

// TriggerDownload tells Unsplash a photo was chosen, by requesting its download location. Only a location on Unsplash's API host over https is requested, so the endpoint cannot be pointed at another server with the access key attached.
func (s *Service) TriggerDownload(ctx context.Context, userID uuid.UUID, downloadLocation string) error {
	if err := s.begin(ctx, userID); err != nil {
		return err
	}
	u, err := url.Parse(downloadLocation)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, s.base.Host) || u.User != nil {
		return apperr.BadRequest(MessageInvalidURL)
	}
	if err := s.get(ctx, u.String(), nil); err != nil {
		return fmt.Errorf("trigger unsplash download: %w", err)
	}
	return nil
}

// get requests target with the access key in the Authorization header, which keeps it out of the URL and so out of any logged error, and decodes a JSON answer into out when out is not nil.
func (s *Service) get(ctx context.Context, target string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Client-ID "+string(s.accessKey))
	req.Header.Set("Accept-Version", "v1")
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body := io.LimitReader(res.Body, maxResponseBytes)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var failure struct {
			Errors []string `json:"errors"`
		}
		_ = json.NewDecoder(body).Decode(&failure)
		return fmt.Errorf("unsplash answered %d: %s", res.StatusCode, strings.Join(failure.Errors, "; "))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, body)
		return nil
	}
	return json.NewDecoder(body).Decode(out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// encodeComponent percent-encodes an attribution parameter as forma2's encodeURIComponent did, a space as %20 rather than +. It also encodes !'()*, which encodeURIComponent leaves alone; both decode to the same text.
func encodeComponent(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
