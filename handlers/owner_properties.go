package handlers

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renderyourworld/parcel_heatmap/db"
)

const (
	defaultOwnerPropertiesPageSize = 25
	maxOwnerPropertiesPageSize     = 100
	ownerPropertiesCacheTTL        = 15 * time.Minute
	ownerPropertiesCacheMaxEntries = 256
)

var (
	nonAlphaNumRegex    = regexp.MustCompile(`[^A-Z0-9\s]`)
	spaceCollapseRegex  = regexp.MustCompile(`\s+`)
	zipRegex            = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\b`)
	houseNumberRegex    = regexp.MustCompile(`\b(\d+)\b`)
	poBoxRegex          = regexp.MustCompile(`\b(?:P\s*O\s*BOX|POBOX|PMB)\s*(\d+)\b`)
	streetSuffixPattern = map[string]string{
		"ROAD":      "RD",
		"STREET":    "ST",
		"AVENUE":    "AVE",
		"DRIVE":     "DR",
		"LANE":      "LN",
		"COURT":     "CT",
		"CIRCLE":    "CIR",
		"HIGHWAY":   "HWY",
		"PARKWAY":   "PKWY",
		"TERRACE":   "TER",
		"PLACE":     "PL",
		"TRAIL":     "TRL",
		"BOULEVARD": "BLVD",
		"NORTH":     "N",
		"SOUTH":     "S",
		"EAST":      "E",
		"WEST":      "W",
		"NORTHEAST": "NE",
		"NORTHWEST": "NW",
		"SOUTHEAST": "SE",
		"SOUTHWEST": "SW",
	}
)

type ownerPropertiesCacheEntry struct {
	payload   []byte
	expiresAt time.Time
}

var ownerPropertiesCache = struct {
	mu   sync.RWMutex
	data map[string]ownerPropertiesCacheEntry
}{
	data: make(map[string]ownerPropertiesCacheEntry),
}

type ownerPropertyRow struct {
	FeatureID      string   `gorm:"column:feature_id" json:"feature_id"`
	CountyName     string   `gorm:"column:county_name" json:"county_name"`
	SiteAddress    *string  `gorm:"column:site_address" json:"site_address"`
	Classification *string  `gorm:"column:classification" json:"classification"`
	Category       *string  `gorm:"column:category" json:"category"`
	TaxDistrict    *string  `gorm:"column:tax_district" json:"tax_district"`
	Acres          *float64 `gorm:"column:acres" json:"acres"`
	Lat            float64  `gorm:"column:lat" json:"lat"`
	Lng            float64  `gorm:"column:lng" json:"lng"`
	OwnerName      string   `json:"owner_name"`
	OwnerAddress   string   `json:"owner_address"`
	AddressMissing bool     `json:"address_missing"`

	MatchConfidence int    `json:"match_confidence"`
	MatchBand       string `json:"match_band"`
}

type ownerAnchor struct {
	FeatureID    string
	CountyID     int
	OwnerName    string
	OwnerAddress string
	Lat          float64
	Lng          float64
}

type ownerAddressParts struct {
	Canonical         string
	StreetStrict      string
	StreetRelaxed     string
	House             string
	City              string
	Zip5              string
	StrictAddressKey  string
	RelaxedAddressKey string
	IsPOBox           bool
	POBox             string
}

type ownerCandidateRow struct {
	FeatureID      string   `gorm:"column:feature_id"`
	CountyID       int      `gorm:"column:county_id"`
	CountyName     string   `gorm:"column:county_name"`
	SiteAddress    *string  `gorm:"column:site_address"`
	Classification *string  `gorm:"column:classification"`
	Category       *string  `gorm:"column:category"`
	TaxDistrict    *string  `gorm:"column:tax_district"`
	Acres          *float64 `gorm:"column:acres"`
	Lat            float64  `gorm:"column:lat"`
	Lng            float64  `gorm:"column:lng"`
	OwnerName      string   `gorm:"column:owner_name"`
	OwnerAddress   *string  `gorm:"column:owner_address"`
}

type ownerMaterializedRow struct {
	FeatureID       string   `gorm:"column:feature_id"`
	CountyName      string   `gorm:"column:county_name"`
	SiteAddress     *string  `gorm:"column:site_address"`
	Classification  *string  `gorm:"column:classification"`
	Category        *string  `gorm:"column:category"`
	TaxDistrict     *string  `gorm:"column:tax_district"`
	Acres           *float64 `gorm:"column:acres"`
	Lat             float64  `gorm:"column:lat"`
	Lng             float64  `gorm:"column:lng"`
	OwnerName       *string  `gorm:"column:owner_name"`
	OwnerAddress    *string  `gorm:"column:owner_address"`
	MatchConfidence int      `gorm:"column:match_confidence"`
	MatchBand       string   `gorm:"column:match_band"`
}

func ownerPropertiesCacheKey(anchorFeatureID, ownerNorm string, loadAll bool, page, pageSize int) string {
	return anchorFeatureID + "|" + ownerNorm + "|" + strconv.FormatBool(loadAll) + "|" + strconv.Itoa(page) + "|" + strconv.Itoa(pageSize)
}

func getCachedOwnerProperties(key string) ([]byte, bool) {
	now := time.Now()
	ownerPropertiesCache.mu.RLock()
	entry, ok := ownerPropertiesCache.data[key]
	ownerPropertiesCache.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			ownerPropertiesCache.mu.Lock()
			delete(ownerPropertiesCache.data, key)
			ownerPropertiesCache.mu.Unlock()
		}
		return nil, false
	}
	return entry.payload, true
}

func putCachedOwnerProperties(key string, payload []byte) {
	ownerPropertiesCache.mu.Lock()
	defer ownerPropertiesCache.mu.Unlock()

	if len(ownerPropertiesCache.data) >= ownerPropertiesCacheMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		first := true
		for k, v := range ownerPropertiesCache.data {
			if first || v.expiresAt.Before(oldestExpiry) {
				oldestKey = k
				oldestExpiry = v.expiresAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(ownerPropertiesCache.data, oldestKey)
		}
	}

	ownerPropertiesCache.data[key] = ownerPropertiesCacheEntry{
		payload:   payload,
		expiresAt: time.Now().Add(ownerPropertiesCacheTTL),
	}
}

func parseFeatureID(featureID string) (int, int64, bool) {
	parts := strings.SplitN(strings.TrimSpace(featureID), "_", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	countyID, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	objectID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return countyID, objectID, true
}

func normalizeSpaces(s string) string {
	return strings.TrimSpace(spaceCollapseRegex.ReplaceAllString(s, " "))
}

func normalizeOwnerName(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return ""
	}
	u = strings.ReplaceAll(u, "&", " AND ")
	u = nonAlphaNumRegex.ReplaceAllString(u, " ")
	tokens := strings.Fields(normalizeSpaces(u))
	filtered := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		switch tok {
		case "ET", "AL", "TR", "TRUST", "LLC", "INC", "LTD":
			continue
		default:
			filtered = append(filtered, tok)
		}
	}
	return strings.Join(filtered, " ")
}

func surnameToken(name string) string {
	toks := strings.Fields(name)
	for _, tok := range toks {
		if tok != "AND" {
			return tok
		}
	}
	return ""
}

func tokenSubsetRatio(subsetText, supersetText string) float64 {
	subsetTokens := strings.Fields(subsetText)
	supersetTokens := strings.Fields(supersetText)
	if len(subsetTokens) == 0 || len(supersetTokens) == 0 {
		return 0
	}
	super := make(map[string]struct{}, len(supersetTokens))
	for _, t := range supersetTokens {
		super[t] = struct{}{}
	}
	matched := 0
	for _, t := range subsetTokens {
		if _, ok := super[t]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(subsetTokens))
}

func normalizeOwnerAddress(s string) ownerAddressParts {
	raw := strings.TrimSpace(s)
	u := strings.ToUpper(raw)
	if strings.TrimSpace(u) == "" {
		return ownerAddressParts{}
	}
	u = strings.ReplaceAll(u, "&", " AND ")
	zip5 := ""
	if m := zipRegex.FindStringSubmatch(u); len(m) > 1 {
		zip5 = m[1]
	}
	poBox := ""
	if m := poBoxRegex.FindStringSubmatch(u); len(m) > 1 {
		poBox = m[1]
	}

	parts := strings.Split(u, ",")
	streetRaw := ""
	cityRaw := ""
	if len(parts) > 0 {
		streetRaw = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		cityRaw = strings.TrimSpace(parts[1])
	}

	streetRaw = nonAlphaNumRegex.ReplaceAllString(streetRaw, " ")
	streetRaw = normalizeSpaces(streetRaw)
	cityRaw = nonAlphaNumRegex.ReplaceAllString(cityRaw, " ")
	cityRaw = normalizeSpaces(cityRaw)
	if streetRaw == "" && cityRaw == "" {
		return ownerAddressParts{}
	}

	toks := strings.Fields(streetRaw)
	for i := range toks {
		if replacement, ok := streetSuffixPattern[toks[i]]; ok {
			toks[i] = replacement
		}
	}
	canonicalStreet := strings.Join(toks, " ")
	house := ""
	if m := houseNumberRegex.FindStringSubmatch(canonicalStreet); len(m) > 1 {
		house = m[1]
	}

	streetTokens := toks
	if house != "" {
		trimmed := strings.TrimPrefix(canonicalStreet, house)
		streetTokens = strings.Fields(strings.TrimSpace(trimmed))
	}

	isDirectional := func(tok string) bool {
		switch tok {
		case "N", "S", "E", "W", "NE", "NW", "SE", "SW":
			return true
		default:
			return false
		}
	}

	streetStrict := normalizeSpaces(strings.Join(streetTokens, " "))
	relaxedTokens := make([]string, 0, len(streetTokens))
	for _, tok := range streetTokens {
		if isDirectional(tok) {
			continue
		}
		relaxedTokens = append(relaxedTokens, tok)
	}
	streetRelaxed := normalizeSpaces(strings.Join(relaxedTokens, " "))
	if streetRelaxed == "" {
		streetRelaxed = streetStrict
	}

	cityNorm := cityRaw
	if cityNorm != "" {
		cityTokens := strings.Fields(cityNorm)
		normalizedCityTokens := make([]string, 0, len(cityTokens))
		for _, tok := range cityTokens {
			if tok == "GA" || tok == "GEORGIA" {
				continue
			}
			normalizedCityTokens = append(normalizedCityTokens, tok)
		}
		cityNorm = normalizeSpaces(strings.Join(normalizedCityTokens, " "))
	}

	strictAddressKey := normalizeSpaces(strings.Join([]string{house, streetStrict, cityNorm, zip5}, "|"))
	relaxedAddressKey := normalizeSpaces(strings.Join([]string{house, streetRelaxed, cityNorm, zip5}, "|"))

	canonical := normalizeSpaces(strings.Join([]string{house, streetStrict, cityNorm, zip5}, " "))
	return ownerAddressParts{
		Canonical:         canonical,
		StreetStrict:      streetStrict,
		StreetRelaxed:     streetRelaxed,
		House:             house,
		City:              cityNorm,
		Zip5:              zip5,
		StrictAddressKey:  strictAddressKey,
		RelaxedAddressKey: relaxedAddressKey,
		IsPOBox:           strings.Contains(canonicalStreet, "PO BOX") || strings.Contains(canonicalStreet, "PMB"),
		POBox:             poBox,
	}
}

func tokenOverlapRatio(a, b string) float64 {
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	aset := make(map[string]struct{}, len(ta))
	for _, t := range ta {
		aset[t] = struct{}{}
	}
	common := 0
	for _, t := range tb {
		if _, ok := aset[t]; ok {
			common++
		}
	}
	denom := len(ta)
	if len(tb) > denom {
		denom = len(tb)
	}
	return float64(common) / float64(denom)
}

func haversineMiles(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusMiles = 3958.8
	toRad := math.Pi / 180
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	lat1Rad := lat1 * toRad
	lat2Rad := lat2 * toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMiles * c
}

func confidenceBand(score int) string {
	if score >= 85 {
		return "high"
	}
	if score >= 55 {
		return "medium"
	}
	return "low"
}

func scoreCandidate(anchor ownerAnchor, candidate ownerCandidateRow) (int, []string) {
	score := 0
	addressScore := 0
	reasons := make([]string, 0, 6)
	anchorAddr := normalizeOwnerAddress(anchor.OwnerAddress)
	candidateAddr := normalizeOwnerAddress(nullSafeString(candidate.OwnerAddress))
	anchorNameNorm := normalizeOwnerName(anchor.OwnerName)
	candidateNameNorm := normalizeOwnerName(candidate.OwnerName)

	switch {
	case anchorAddr.StrictAddressKey != "" &&
		candidateAddr.StrictAddressKey != "" &&
		anchorAddr.StrictAddressKey == candidateAddr.StrictAddressKey:
		score += 70
		addressScore += 70
		reasons = append(reasons, "Owner address exact (strict)")
	case anchorAddr.RelaxedAddressKey != "" &&
		candidateAddr.RelaxedAddressKey != "" &&
		anchorAddr.RelaxedAddressKey == candidateAddr.RelaxedAddressKey &&
		!anchorAddr.IsPOBox &&
		!candidateAddr.IsPOBox:
		score += 62
		addressScore += 62
		reasons = append(reasons, "Owner address exact (relaxed)")
	case anchorAddr.House != "" &&
		anchorAddr.House == candidateAddr.House &&
		anchorAddr.StreetRelaxed != "" &&
		anchorAddr.StreetRelaxed == candidateAddr.StreetRelaxed &&
		anchorAddr.Zip5 != "" &&
		anchorAddr.Zip5 == candidateAddr.Zip5:
		score += 50
		addressScore += 50
		reasons = append(reasons, "Owner house+street+zip match")
	case anchorAddr.House != "" &&
		anchorAddr.House == candidateAddr.House &&
		anchorAddr.StreetRelaxed != "" &&
		anchorAddr.StreetRelaxed == candidateAddr.StreetRelaxed &&
		anchorAddr.City != "" &&
		anchorAddr.City == candidateAddr.City:
		score += 35
		addressScore += 35
		reasons = append(reasons, "Owner house+street+city match")
	case anchorAddr.Zip5 != "" && anchorAddr.Zip5 == candidateAddr.Zip5:
		score += 15
		addressScore += 15
		reasons = append(reasons, "Owner ZIP match")
	}
	if anchorAddr.IsPOBox || candidateAddr.IsPOBox {
		score -= 25
		reasons = append(reasons, "PO Box ambiguity penalty")
		if anchorAddr.IsPOBox && candidateAddr.IsPOBox && anchorAddr.POBox != "" && candidateAddr.POBox != "" && anchorAddr.POBox != candidateAddr.POBox {
			score -= 20
			reasons = append(reasons, "Different PO Box penalty")
		}
	}

	if anchorNameNorm != "" && candidateNameNorm != "" {
		if anchorNameNorm == candidateNameNorm {
			score += 30
			reasons = append(reasons, "Owner name exact")
		} else {
			ratio := tokenOverlapRatio(anchorNameNorm, candidateNameNorm)
			subsetRatio := tokenSubsetRatio(anchorNameNorm, candidateNameNorm)
			switch {
			case subsetRatio >= 0.99:
				score += 22
				reasons = append(reasons, "Owner name subset match")
			case ratio >= 0.90:
				score += 22
				reasons = append(reasons, "Owner name very similar")
			case ratio >= 0.75:
				score += 16
				reasons = append(reasons, "Owner name similar")
			case ratio >= 0.60:
				score += 8
				reasons = append(reasons, "Owner name partial overlap")
			}
		}
		anchorSurname := surnameToken(anchorNameNorm)
		candidateSurname := surnameToken(candidateNameNorm)
		if anchorSurname != "" &&
			candidateSurname != "" &&
			len(anchorSurname) > 1 &&
			len(candidateSurname) > 1 &&
			anchorSurname != candidateSurname &&
			addressScore < 50 {
			score -= 10
			reasons = append(reasons, "Surname mismatch penalty")
		}
	}

	// County is only a tiebreaker when address evidence is weak.
	if addressScore < 35 && anchor.CountyID > 0 && candidate.CountyID == anchor.CountyID {
		score += 6
		reasons = append(reasons, "Same county")
	}

	if anchor.Lat != 0 && anchor.Lng != 0 && candidate.Lat != 0 && candidate.Lng != 0 {
		if haversineMiles(anchor.Lat, anchor.Lng, candidate.Lat, candidate.Lng) < 2 {
			score += 4
			reasons = append(reasons, "Nearby parcel")
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, reasons
}

func nullSafeString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func loadAnchorFromFeatureID(featureID string) (ownerAnchor, bool) {
	countyID, objectID, ok := parseFeatureID(featureID)
	if !ok {
		return ownerAnchor{}, false
	}
	var row struct {
		OwnerName    *string  `gorm:"column:owner_name"`
		OwnerAddress *string  `gorm:"column:owner_address"`
		Lat          *float64 `gorm:"column:search_lat"`
		Lng          *float64 `gorm:"column:search_lng"`
	}
	if err := db.DB.Raw(`
		SELECT
			p.owner_name,
			p.owner_address,
			p.search_lat,
			p.search_lng
		FROM parcels p
		WHERE p.processed IS NULL
		  AND p.county_id = ?
		  AND p.objectid = ?
		LIMIT 1
	`, countyID, objectID).Scan(&row).Error; err != nil {
		return ownerAnchor{}, false
	}
	anchor := ownerAnchor{
		FeatureID:    featureID,
		CountyID:     countyID,
		OwnerName:    nullSafeString(row.OwnerName),
		OwnerAddress: nullSafeString(row.OwnerAddress),
	}
	if row.Lat != nil {
		anchor.Lat = *row.Lat
	}
	if row.Lng != nil {
		anchor.Lng = *row.Lng
	}
	return anchor, anchor.OwnerName != "" || anchor.OwnerAddress != ""
}

func loadOwnerPropertiesFromMaterialized(anchor ownerAnchor) ([]ownerPropertyRow, bool, error) {
	if strings.TrimSpace(anchor.FeatureID) == "" {
		return nil, false, nil
	}
	countyID, objectID, ok := parseFeatureID(anchor.FeatureID)
	if !ok {
		return nil, false, nil
	}

	var groupID int64
	err := db.DB.Raw(`
		SELECT ogm.group_id
		FROM owner_group_members ogm
		JOIN parcels p
		  ON p.id = ogm.parcel_id
		WHERE p.processed IS NULL
		  AND p.county_id = ?
		  AND p.objectid = ?
		LIMIT 1
	`, countyID, objectID).Row().Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}

	var rows []ownerMaterializedRow
	if err := db.DB.Raw(`
		SELECT
			p.county_id || '_' || p.objectid AS feature_id,
			co.name AS county_name,
			p.site_address,
			p.classification,
			cc.category,
			p.tax_district,
			p.acres::float8 AS acres,
			p.search_lat AS lat,
			p.search_lng AS lng,
			p.owner_name,
			p.owner_address,
			ogm.match_confidence,
			ogm.match_band
		FROM owner_group_members ogm
		JOIN parcels p
		  ON p.id = ogm.parcel_id
		JOIN counties co
		  ON co.id = p.county_id
		LEFT JOIN parcel_class_codes cc
		  ON cc.county_id = p.county_id
		 AND cc.code = p.classification
		WHERE ogm.group_id = ?
		  AND p.processed IS NULL
		  AND p.objectid IS NOT NULL
		  AND p.search_lat IS NOT NULL
		  AND p.search_lng IS NOT NULL
		ORDER BY ogm.match_confidence DESC, co.name ASC, p.site_address ASC NULLS LAST, p.id ASC
	`, groupID).Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	results := make([]ownerPropertyRow, 0, len(rows))
	for _, r := range rows {
		ownerName := strings.TrimSpace(nullSafeString(r.OwnerName))
		if ownerName == "" {
			ownerName = anchor.OwnerName
		}
		results = append(results, ownerPropertyRow{
			FeatureID:       r.FeatureID,
			CountyName:      r.CountyName,
			SiteAddress:     normalizeNullableString(r.SiteAddress),
			Classification:  normalizeNullableString(r.Classification),
			Category:        normalizeNullableString(r.Category),
			TaxDistrict:     normalizeNullableString(r.TaxDistrict),
			Acres:           r.Acres,
			Lat:             r.Lat,
			Lng:             r.Lng,
			OwnerName:       ownerName,
			OwnerAddress:    nullSafeString(r.OwnerAddress),
			AddressMissing:  r.SiteAddress == nil || strings.TrimSpace(*r.SiteAddress) == "",
			MatchConfidence: r.MatchConfidence,
			MatchBand:       r.MatchBand,
		})
	}
	return results, true, nil
}

// GetOwnerProperties returns statewide parcels for a selected owner context.
// Preferred format: /api/owners/properties?feature_id={county_id}_{objectid}
// Fallback format: /api/owners/properties?owner_name=...
func GetOwnerProperties(c *gin.Context) {
	featureID := strings.TrimSpace(c.Query("feature_id"))
	ownerName := strings.TrimSpace(c.Query("owner_name"))
	ownerAddress := strings.TrimSpace(c.Query("owner_address"))
	if featureID == "" && ownerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "feature_id or owner_name is required"})
		return
	}

	page := 1
	if raw := c.Query("page"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			page = v
		}
	}

	pageSize := defaultOwnerPropertiesPageSize
	if raw := c.Query("page_size"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			pageSize = v
		}
	}
	if pageSize > maxOwnerPropertiesPageSize {
		pageSize = maxOwnerPropertiesPageSize
	}
	loadAll := true

	anchor := ownerAnchor{
		FeatureID:    featureID,
		OwnerName:    ownerName,
		OwnerAddress: ownerAddress,
	}
	if featureID != "" {
		if resolved, ok := loadAnchorFromFeatureID(featureID); ok {
			anchor = resolved
		}
	}
	if anchor.OwnerName == "" && ownerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no owner context found for feature_id"})
		return
	}
	if anchor.OwnerName == "" {
		anchor.OwnerName = ownerName
	}
	if anchor.OwnerAddress == "" {
		anchor.OwnerAddress = ownerAddress
	}

	normalizedOwner := strings.ToLower(normalizeSpaces(anchor.OwnerName))
	cacheKey := ownerPropertiesCacheKey(featureID, normalizedOwner, loadAll, page, pageSize)
	if cachedPayload, hit := getCachedOwnerProperties(cacheKey); hit {
		c.Header("Cache-Control", "public, max-age=300")
		c.Header("X-Cache", "HIT")
		c.Data(http.StatusOK, "application/json", cachedPayload)
		return
	}

	if materializedRows, ok, err := loadOwnerPropertiesFromMaterialized(anchor); err == nil && ok && len(materializedRows) >= 2 {
		counties := make(map[string]struct{}, len(materializedRows))
		for _, r := range materializedRows {
			counties[r.CountyName] = struct{}{}
		}
		page = 1
		pageSize = len(materializedRows)
		if pageSize == 0 {
			pageSize = defaultOwnerPropertiesPageSize
		}
		responseBody := map[string]interface{}{
			"owner_name":        anchor.OwnerName,
			"owner_address":     anchor.OwnerAddress,
			"feature_id":        anchor.FeatureID,
			"total_properties":  int64(len(materializedRows)),
			"county_count":      int64(len(counties)),
			"page":              page,
			"page_size":         pageSize,
			"total_pages":       1,
			"results":           materializedRows,
			"match_type":        "owner_group_materialized",
			"statewide_scope":   true,
			"includes_no_situs": true,
		}
		payload, marshalErr := json.Marshal(responseBody)
		if marshalErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode owner properties"})
			return
		}
		putCachedOwnerProperties(cacheKey, payload)
		c.Header("Cache-Control", "public, max-age=300")
		c.Header("X-Cache", "MISS")
		c.Data(http.StatusOK, "application/json", payload)
		return
	} else if err != nil {
		// If materialized tables are unavailable/empty, fall back to dynamic matching.
	}

	anchorNameNorm := strings.ToLower(normalizeOwnerName(anchor.OwnerName))
	anchorSurname := ""
	if parts := strings.Fields(anchorNameNorm); len(parts) > 0 {
		anchorSurname = parts[0]
	}
	addressParts := normalizeOwnerAddress(anchor.OwnerAddress)
	houseLike := "%"
	zipLike := "%"
	if addressParts.House != "" {
		houseLike = "%" + addressParts.House + "%"
	}
	if addressParts.Zip5 != "" {
		zipLike = "%" + addressParts.Zip5 + "%"
	}

	query := `
		SELECT
			p.county_id || '_' || p.objectid AS feature_id,
			p.county_id,
			co.name AS county_name,
			p.site_address,
			p.classification,
			cc.category,
			p.tax_district,
			p.acres::float8 AS acres,
			p.search_lat AS lat,
			p.search_lng AS lng,
			COALESCE(p.owner_name, '') AS owner_name,
			p.owner_address
		FROM parcels p
		JOIN counties co
		  ON co.id = p.county_id
		LEFT JOIN parcel_class_codes cc
		  ON cc.county_id = p.county_id
		 AND cc.code = p.classification
		WHERE p.processed IS NULL
		  AND p.objectid IS NOT NULL
		  AND p.search_lat IS NOT NULL
		  AND p.search_lng IS NOT NULL
		  AND (
		    (? <> '' AND lower(COALESCE(p.owner_name, '')) = ?)
		    OR (? <> '' AND lower(COALESCE(p.owner_name, '')) LIKE ? || '%')
		    OR (? <> '' AND lower(trim(COALESCE(p.owner_address, ''))) = lower(trim(?)))
		    OR (? <> '' AND ? <> '' AND lower(COALESCE(p.owner_address, '')) LIKE ? AND lower(COALESCE(p.owner_address, '')) LIKE ?)
		  )
		ORDER BY
			CASE WHEN ? <> '' AND lower(COALESCE(p.owner_name, '')) = ? THEN 0 ELSE 1 END,
			CASE WHEN ? <> '' AND lower(COALESCE(p.owner_name, '')) LIKE ? || '%' THEN 0 ELSE 1 END,
			co.name ASC,
			p.site_address ASC NULLS LAST,
			p.id ASC
		LIMIT 700
	`
	args := []interface{}{
		normalizedOwner, normalizedOwner,
		anchorSurname, anchorSurname,
		anchor.OwnerAddress, anchor.OwnerAddress,
		addressParts.House, addressParts.Zip5, houseLike, zipLike,
		normalizedOwner, normalizedOwner,
		anchorSurname, anchorSurname,
	}

	var candidates []ownerCandidateRow
	if err := db.DB.Raw(query, args...).Scan(&candidates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query owner properties"})
		return
	}

	filtered := make([]ownerPropertyRow, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, row := range candidates {
		if _, exists := seen[row.FeatureID]; exists {
			continue
		}
		confidence, _ := scoreCandidate(anchor, row)
		keep := confidence >= 45 || row.FeatureID == anchor.FeatureID
		if !keep {
			continue
		}
		seen[row.FeatureID] = struct{}{}
		filtered = append(filtered, ownerPropertyRow{
			FeatureID:       row.FeatureID,
			CountyName:      row.CountyName,
			SiteAddress:     normalizeNullableString(row.SiteAddress),
			Classification:  normalizeNullableString(row.Classification),
			Category:        normalizeNullableString(row.Category),
			TaxDistrict:     normalizeNullableString(row.TaxDistrict),
			Acres:           row.Acres,
			Lat:             row.Lat,
			Lng:             row.Lng,
			OwnerName:       row.OwnerName,
			OwnerAddress:    nullSafeString(row.OwnerAddress),
			AddressMissing:  row.SiteAddress == nil || strings.TrimSpace(*row.SiteAddress) == "",
			MatchConfidence: confidence,
			MatchBand:       confidenceBand(confidence),
		})
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].MatchConfidence != filtered[j].MatchConfidence {
			return filtered[i].MatchConfidence > filtered[j].MatchConfidence
		}
		if filtered[i].CountyName != filtered[j].CountyName {
			return filtered[i].CountyName < filtered[j].CountyName
		}
		return strings.ToLower(nullSafeString(filtered[i].SiteAddress)) < strings.ToLower(nullSafeString(filtered[j].SiteAddress))
	})

	counties := make(map[string]struct{}, len(filtered))
	for _, r := range filtered {
		counties[r.CountyName] = struct{}{}
	}
	totalProperties := int64(len(filtered))
	countyCount := int64(len(counties))
	page = 1
	pageSize = len(filtered)
	if pageSize == 0 {
		pageSize = defaultOwnerPropertiesPageSize
	}

	responseBody := map[string]interface{}{
		"owner_name":        anchor.OwnerName,
		"owner_address":     anchor.OwnerAddress,
		"feature_id":        anchor.FeatureID,
		"total_properties":  totalProperties,
		"county_count":      countyCount,
		"page":              page,
		"page_size":         pageSize,
		"total_pages":       1,
		"results":           filtered,
		"match_type":        "hybrid_scored",
		"statewide_scope":   true,
		"includes_no_situs": true,
	}
	payload, err := json.Marshal(responseBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode owner properties"})
		return
	}
	putCachedOwnerProperties(cacheKey, payload)
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("X-Cache", "MISS")
	c.Data(http.StatusOK, "application/json", payload)
}

func normalizeNullableString(v *string) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return &s
}
