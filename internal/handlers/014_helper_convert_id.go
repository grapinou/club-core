package handlers

import "strconv"

func parseID(rawID string) (int32, error) {
	id, err := strconv.ParseInt(rawID, 10, 32)
	if err != nil {
		return 0, err
	}

	return int32(id), nil
}
