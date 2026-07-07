package utils

func MaskPassword(password string) string {
	if password == "" {
		return ""
	}
	if len(password) <= 4 {
		return "****"
	}
	return password[:2] + "****" + password[len(password)-2:]
}

func MaskEmail(email string) string {
	if email == "" {
		return ""
	}
	parts := splitEmail(email)
	if len(parts) != 2 {
		return "****@****.com"
	}
	username := parts[0]
	domain := parts[1]
	
	if len(username) <= 2 {
		return "**@" + domain
	}
	return username[:2] + "****@" + domain
}

func MaskPhone(phone string) string {
	if phone == "" {
		return ""
	}
	if len(phone) <= 4 {
		return "****"
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

func MaskCreditCard(card string) string {
	if card == "" {
		return ""
	}
	if len(card) <= 4 {
		return "****"
	}
	return "****-****-****-" + card[len(card)-4:]
}

func splitEmail(email string) []string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return []string{}
}
