package domain_api

import (
	"database/sql"
)

// DomainDAO extends the existing DAO functionality
type DomainDAO interface {
	UpsertDomain(domain, originIP string) error
	DeleteDomain(domain string) error
	GetAllDomains() ([]DomainMapping, error)
	GetDomainByName(domain string) (string, error)
}

// DomainDAOImpl implements the DomainDAO interface
type DomainDAOImpl struct {
	db *sql.DB
}

// DomainMapping represents the mapping of a domain to an IP address
type DomainMapping struct {
	Domain   string `json:"domain"`
	OriginIP string `json:"origin_ip"`
}

// NewDomainDAO creates a new instance of DomainDAO
func NewDomainDAO(db *sql.DB) DomainDAO {
	return &DomainDAOImpl{db: db}
}

// UpsertDomain inserts or updates the domain mapping
func (d *DomainDAOImpl) UpsertDomain(domain, originIP string) error {
	query := `INSERT INTO domain_origin (domain, origin_ip) 
              VALUES (?, ?) 
              ON DUPLICATE KEY UPDATE origin_ip = ?`

	_, err := d.db.Exec(query, domain, originIP, originIP)
	return err
}

// DeleteDomain removes the domain mapping
func (d *DomainDAOImpl) DeleteDomain(domain string) error {
	query := `DELETE FROM domain_origin WHERE domain = ?`

	_, err := d.db.Exec(query, domain)
	return err
}

// GetAllDomains retrieves all domain mappings
func (d *DomainDAOImpl) GetAllDomains() ([]DomainMapping, error) {
	var domains []DomainMapping
	query := "SELECT domain, origin_ip FROM domain_origin"

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var domain DomainMapping
		if err := rows.Scan(&domain.Domain, &domain.OriginIP); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}

	return domains, nil
}

// GetDomainByName retrieves the target IP for a specific domain
func (d *DomainDAOImpl) GetDomainByName(domain string) (string, error) {
	var originIP string
	query := "SELECT origin_ip FROM domain_origin WHERE domain = ?"

	err := d.db.QueryRow(query, domain).Scan(&originIP)
	if err != nil {
		return "", err
	}

	return originIP, nil
}
