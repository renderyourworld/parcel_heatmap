package importers

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/renderyourworld/parcel_heatmap/models"
	"gorm.io/gorm"
)

const ownerGroupsBatchSize = 10000

var (
	ownerNonAlphaNumRegex   = regexp.MustCompile(`[^A-Z0-9\s]`)
	ownerSpaceCollapseRegex = regexp.MustCompile(`\s+`)
	ownerZipRegex           = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\b`)
	ownerHouseNumberRegex   = regexp.MustCompile(`\b(\d+)\b`)
	ownerPoBoxRegex         = regexp.MustCompile(`\b(?:P\s*O\s*BOX|POBOX|PMB)\s*(\d+)\b`)
	ownerStreetSuffixMap    = map[string]string{
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

type ownerGroupAddressParts struct {
	RelaxedAddressKey string
	Zip5              string
	IsPOBox           bool
	POBox             string
}

type ownerGroupParcelRow struct {
	ID           uint64  `gorm:"column:id"`
	CountyID     int     `gorm:"column:county_id"`
	ObjectID     int64   `gorm:"column:objectid"`
	OwnerName    *string `gorm:"column:owner_name"`
	OwnerAddress *string `gorm:"column:owner_address"`
}

func normalizeOwnerGroupSpaces(s string) string {
	return strings.TrimSpace(ownerSpaceCollapseRegex.ReplaceAllString(s, " "))
}

func normalizeOwnerGroupName(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return ""
	}
	u = strings.ReplaceAll(u, "&", " AND ")
	u = ownerNonAlphaNumRegex.ReplaceAllString(u, " ")
	tokens := strings.Fields(normalizeOwnerGroupSpaces(u))
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

func normalizeOwnerGroupAddress(s string) ownerGroupAddressParts {
	raw := strings.TrimSpace(s)
	u := strings.ToUpper(raw)
	if strings.TrimSpace(u) == "" {
		return ownerGroupAddressParts{}
	}
	u = strings.ReplaceAll(u, "&", " AND ")
	zip5 := ""
	if m := ownerZipRegex.FindStringSubmatch(u); len(m) > 1 {
		zip5 = m[1]
	}
	poBox := ""
	if m := ownerPoBoxRegex.FindStringSubmatch(u); len(m) > 1 {
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

	streetRaw = ownerNonAlphaNumRegex.ReplaceAllString(streetRaw, " ")
	streetRaw = normalizeOwnerGroupSpaces(streetRaw)
	cityRaw = ownerNonAlphaNumRegex.ReplaceAllString(cityRaw, " ")
	cityRaw = normalizeOwnerGroupSpaces(cityRaw)
	if streetRaw == "" && cityRaw == "" {
		return ownerGroupAddressParts{}
	}

	toks := strings.Fields(streetRaw)
	for i := range toks {
		if replacement, ok := ownerStreetSuffixMap[toks[i]]; ok {
			toks[i] = replacement
		}
	}
	canonicalStreet := strings.Join(toks, " ")
	house := ""
	if m := ownerHouseNumberRegex.FindStringSubmatch(canonicalStreet); len(m) > 1 {
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

	relaxedTokens := make([]string, 0, len(streetTokens))
	for _, tok := range streetTokens {
		if isDirectional(tok) {
			continue
		}
		relaxedTokens = append(relaxedTokens, tok)
	}
	streetRelaxed := normalizeOwnerGroupSpaces(strings.Join(relaxedTokens, " "))
	if streetRelaxed == "" {
		streetRelaxed = normalizeOwnerGroupSpaces(strings.Join(streetTokens, " "))
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
		cityNorm = normalizeOwnerGroupSpaces(strings.Join(normalizedCityTokens, " "))
	}

	relaxedAddressKey := normalizeOwnerGroupSpaces(strings.Join([]string{house, streetRelaxed, cityNorm, zip5}, "|"))
	return ownerGroupAddressParts{
		RelaxedAddressKey: relaxedAddressKey,
		Zip5:              zip5,
		IsPOBox:           strings.Contains(canonicalStreet, "PO BOX") || strings.Contains(canonicalStreet, "PMB"),
		POBox:             poBox,
	}
}

func ownerGroupBand(confidence int) string {
	if confidence >= 85 {
		return "high"
	}
	if confidence >= 55 {
		return "medium"
	}
	return "low"
}

func ownerGroupKeyFromParcel(row ownerGroupParcelRow) (groupKey, keyType, canonicalName, canonicalAddress string, isPOBox bool, confidence int, reasons []string) {
	ownerName := ""
	if row.OwnerName != nil {
		ownerName = strings.TrimSpace(*row.OwnerName)
	}
	ownerAddress := ""
	if row.OwnerAddress != nil {
		ownerAddress = strings.TrimSpace(*row.OwnerAddress)
	}

	nameNorm := normalizeOwnerGroupName(ownerName)
	addrNorm := normalizeOwnerGroupAddress(ownerAddress)

	switch {
	case addrNorm.RelaxedAddressKey != "" && !addrNorm.IsPOBox:
		return "addr:" + addrNorm.RelaxedAddressKey, "address", ownerName, ownerAddress, false, 94, []string{"Owner address exact (relaxed)", "Materialized group"}
	case addrNorm.IsPOBox && addrNorm.POBox != "":
		return "pobox:" + addrNorm.POBox + "|" + addrNorm.Zip5, "pobox", ownerName, ownerAddress, true, 68, []string{"PO Box grouping", "Materialized group"}
	case nameNorm != "":
		return "name:" + nameNorm, "name", ownerName, ownerAddress, false, 56, []string{"Owner name grouping", "Materialized group"}
	default:
		featureID := fmt.Sprintf("%d_%d", row.CountyID, row.ObjectID)
		return "parcel:" + featureID, "parcel", ownerName, ownerAddress, false, 40, []string{"Parcel fallback grouping"}
	}
}

func resetOwnerGroupsScope(gormDB *gorm.DB, countyFilterID int) error {
	if countyFilterID == 0 {
		if err := gormDB.Exec("TRUNCATE TABLE owner_group_members, owner_groups RESTART IDENTITY").Error; err != nil {
			return fmt.Errorf("failed to truncate owner group tables: %w", err)
		}
		return nil
	}

	if err := gormDB.Exec(`
		DELETE FROM owner_group_members ogm
		USING parcels p
		WHERE ogm.parcel_id = p.id
		  AND p.county_id = ?
	`, countyFilterID).Error; err != nil {
		return fmt.Errorf("failed to clear county owner memberships: %w", err)
	}
	if err := gormDB.Exec(`
		DELETE FROM owner_groups og
		WHERE NOT EXISTS (
			SELECT 1
			FROM owner_group_members ogm
			WHERE ogm.group_id = og.id
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to clear orphan owner groups: %w", err)
	}
	return nil
}

func refreshOwnerGroupCounts(gormDB *gorm.DB) error {
	// Fast-path finalize: skip global member_count synchronization to avoid
	// multi-hour full-table updates on large statewide builds.
	// owner_group_members is the source of truth used by lookup queries.
	var groupCount int64
	var memberCount int64
	_ = gormDB.Raw(`SELECT COUNT(*)::bigint FROM owner_groups`).Scan(&groupCount).Error
	_ = gormDB.Raw(`SELECT COUNT(*)::bigint FROM owner_group_members`).Scan(&memberCount).Error
	log.Printf(
		"owner_groups finalize: fast-path complete (groups=%d memberships=%d; member_count sync skipped)",
		groupCount, memberCount,
	)
	return nil
}

// StartOwnerGroupsBuilder builds materialized owner group relationships for fast reverse-owner lookups.
func StartOwnerGroupsBuilder(gormDB *gorm.DB, countyName string, resume bool, maxParcels int) error {
	countyFilterID := 0
	checkpointCountyName := countyName
	if checkpointCountyName == "" || strings.EqualFold(checkpointCountyName, "all") {
		checkpointCountyName = "all"
	} else {
		var county models.County
		if err := gormDB.Where("name = ?", countyName).First(&county).Error; err != nil {
			return fmt.Errorf("county '%s' not found: %w", countyName, err)
		}
		countyFilterID = int(county.ID)
	}

	checkpoint, err := initializeCheckpoint(gormDB, checkpointCountyName, "owner_groups", resume)
	if err != nil {
		return fmt.Errorf("failed to initialize owner_groups checkpoint: %w", err)
	}

	if !resume {
		if err := resetOwnerGroupsScope(gormDB, countyFilterID); err != nil {
			return err
		}
	}

	currentID := checkpoint.LastProcessedID
	totalProcessed := checkpoint.TotalProcessed
	totalFailed := checkpoint.TotalFailed

	log.Printf(
		"Starting owner_groups build for county=%s (resume=%v, last_processed_id=%d, max=%d)",
		checkpointCountyName, resume, currentID, maxParcels,
	)

	for {
		batchLimit := ownerGroupsBatchSize
		if maxParcels > 0 {
			remaining := maxParcels - totalProcessed
			if remaining <= 0 {
				log.Printf("Reached max owner_groups build limit (%d).", maxParcels)
				break
			}
			if remaining < batchLimit {
				batchLimit = remaining
			}
		}

		var rows []ownerGroupParcelRow
		if err := gormDB.Raw(`
			SELECT
				p.id,
				p.county_id,
				p.objectid,
				p.owner_name,
				p.owner_address
			FROM parcels p
			WHERE p.processed IS NULL
			  AND p.objectid IS NOT NULL
			  AND p.search_lat IS NOT NULL
			  AND p.search_lng IS NOT NULL
			  AND p.id > ?
			  AND (? = 0 OR p.county_id = ?)
			ORDER BY p.id
			LIMIT ?
		`, currentID, countyFilterID, countyFilterID, batchLimit).Scan(&rows).Error; err != nil {
			totalFailed++
			_ = updateCheckpoint(gormDB, checkpointCountyName, "owner_groups", currentID, totalProcessed, totalFailed)
			return fmt.Errorf("failed to load owner group batch at id=%d: %w", currentID, err)
		}
		if len(rows) == 0 {
			break
		}

		tx := gormDB.Begin()
		if tx.Error != nil {
			return fmt.Errorf("failed to begin owner_groups tx: %w", tx.Error)
		}

		maxID := currentID
		for _, row := range rows {
			if int64(row.ID) > maxID {
				maxID = int64(row.ID)
			}

			groupKey, keyType, canonicalName, canonicalAddress, isPOBox, confidence, _ := ownerGroupKeyFromParcel(row)
			var groupID int64
			if err := tx.Raw(`
				INSERT INTO owner_groups (
					group_key,
					key_type,
					canonical_owner_name,
					canonical_owner_address,
					is_po_box,
					updated_at
				)
				VALUES (?, ?, ?, ?, ?, NOW())
				ON CONFLICT (group_key) DO UPDATE SET
					canonical_owner_name = COALESCE(NULLIF(owner_groups.canonical_owner_name, ''), EXCLUDED.canonical_owner_name),
					canonical_owner_address = COALESCE(NULLIF(owner_groups.canonical_owner_address, ''), EXCLUDED.canonical_owner_address),
					is_po_box = owner_groups.is_po_box OR EXCLUDED.is_po_box,
					updated_at = NOW()
				RETURNING id
			`, groupKey, keyType, canonicalName, canonicalAddress, isPOBox).Row().Scan(&groupID); err != nil {
				_ = tx.Rollback()
				totalFailed++
				_ = updateCheckpoint(gormDB, checkpointCountyName, "owner_groups", currentID, totalProcessed, totalFailed)
				return fmt.Errorf("failed to upsert owner group for parcel_id=%d: %w", row.ID, err)
			}

			if err := tx.Exec(`
				INSERT INTO owner_group_members (
					parcel_id,
					group_id,
					match_confidence,
					match_band,
					updated_at
				)
				VALUES (?, ?, ?, ?, NOW())
				ON CONFLICT (parcel_id) DO UPDATE SET
					group_id = EXCLUDED.group_id,
					match_confidence = EXCLUDED.match_confidence,
					match_band = EXCLUDED.match_band,
					updated_at = NOW()
			`, row.ID, groupID, confidence, ownerGroupBand(confidence)).Error; err != nil {
				_ = tx.Rollback()
				totalFailed++
				_ = updateCheckpoint(gormDB, checkpointCountyName, "owner_groups", currentID, totalProcessed, totalFailed)
				return fmt.Errorf("failed to upsert owner group member for parcel_id=%d: %w", row.ID, err)
			}
		}

		if err := tx.Commit().Error; err != nil {
			totalFailed++
			_ = updateCheckpoint(gormDB, checkpointCountyName, "owner_groups", currentID, totalProcessed, totalFailed)
			return fmt.Errorf("failed to commit owner_groups batch at id=%d: %w", currentID, err)
		}

		currentID = maxID
		totalProcessed += len(rows)
		if err := updateCheckpoint(gormDB, checkpointCountyName, "owner_groups", currentID, totalProcessed, totalFailed); err != nil {
			return fmt.Errorf("failed to update owner_groups checkpoint: %w", err)
		}
		log.Printf(
			"owner_groups progress county=%s processed=%d last_processed_id=%d",
			checkpointCountyName, totalProcessed, currentID,
		)
	}

	if err := refreshOwnerGroupCounts(gormDB); err != nil {
		return err
	}
	if err := completeCheckpoint(gormDB, checkpointCountyName, "owner_groups", totalProcessed, totalFailed); err != nil {
		return fmt.Errorf("failed to mark owner_groups checkpoint complete: %w", err)
	}

	log.Printf("owner_groups build complete county=%s total_processed=%d total_failed=%d", checkpointCountyName, totalProcessed, totalFailed)
	return nil
}
