package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/google/uuid"
)

// samlConnection is the identity provider a tenant and product sign in through.
type samlConnection struct {
	ID          uuid.UUID
	Name        string
	IdPEntityID string `gorm:"column:idp_entity_id"`
	IdPMetadata string `gorm:"column:idp_metadata"`
}

// samlConnection returns the connection of a tenant and product, or nil.
func (r *Repository) samlConnection(ctx context.Context, tenant, product string) (*samlConnection, error) {
	var rows []samlConnection
	err := r.db.WithContext(ctx).Raw(
		`SELECT id, name, idp_entity_id, idp_metadata FROM saml_connections WHERE tenant = ? AND product = ?`, tenant, product,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load SAML connection: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// saveSAMLConnection makes the metadata the connection of a tenant and product, replacing the one it had.
func (r *Repository) saveSAMLConnection(ctx context.Context, tenant, product, name, entityID, metadata string) error {
	if err := r.db.WithContext(ctx).Exec(
		`INSERT INTO saml_connections (tenant, product, name, idp_entity_id, idp_metadata) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (tenant, product) DO UPDATE SET name = EXCLUDED.name, idp_entity_id = EXCLUDED.idp_entity_id, idp_metadata = EXCLUDED.idp_metadata, updated_at = now()`,
		tenant, product, name, entityID, metadata,
	).Error; err != nil {
		return fmt.Errorf("save SAML connection: %w", err)
	}
	return nil
}

// PreloadSAMLConnection is forma2's preloadConnection, run at start-up: with SAML enabled, the first *.xml file in dir (by name) becomes the instance's connection, replacing the previous one, so changing the file and restarting switches identity providers. A missing directory or file, or metadata that is not valid, is logged and leaves the stored connection as it was; start-up continues.
func (s *SSO) PreloadSAMLConnection(ctx context.Context, dir string) {
	if !s.samlEnabled() {
		return
	}
	if err := s.preloadSAMLConnection(ctx, dir); err != nil {
		s.log.ErrorContext(ctx, "Error preloading connection", "err", err)
	}
}

func (s *SSO) preloadSAMLConnection(ctx context.Context, dir string) error {
	fsys := os.DirFS(dir)
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read SAML connection directory: %w", err)
	}
	file := ""
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".xml") {
			file = e.Name()
			break
		}
	}
	if file == "" {
		return errors.New("no preloaded connection file found")
	}
	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		return fmt.Errorf("read SAML connection file: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		s.log.InfoContext(ctx, "No preloaded connection metadata found", "file", file)
		return nil
	}
	idp, err := parseIdPMetadata(raw)
	if err != nil {
		return fmt.Errorf("metadata is not valid (%s): %w", file, err)
	}
	if err := s.svc.repo.saveSAMLConnection(ctx, samlTenant, samlProduct, samlConnectionName, idp.EntityID, string(raw)); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "SAML connection loaded", "file", file, "idpEntityId", idp.EntityID)
	return nil
}
