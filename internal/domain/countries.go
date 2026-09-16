package domain

import (
	"sort"
	"strings"
)

// Country is one row of the ISO 3166-1 alpha-2 reference table.
type Country struct {
	Code    string // ISO 3166-1 alpha-2, uppercase
	English string
	Chinese string
}

type countryNames struct{ english, chinese string }

// countryTable is the whole of what the console knows about a country: the
// English label to fall back on and the Chinese name it displays and searches.
// VPN Gate reports an alpha-2 code with every node, so the code is the join key
// and the English name here only stands in for rows whose code is missing.
var countryTable = map[string]countryNames{
	"AD": {"Andorra", "安道尔"},
	"AE": {"United Arab Emirates", "阿联酋"},
	"AF": {"Afghanistan", "阿富汗"},
	"AG": {"Antigua and Barbuda", "安提瓜和巴布达"},
	"AI": {"Anguilla", "安圭拉"},
	"AL": {"Albania", "阿尔巴尼亚"},
	"AM": {"Armenia", "亚美尼亚"},
	"AO": {"Angola", "安哥拉"},
	"AQ": {"Antarctica", "南极洲"},
	"AR": {"Argentina", "阿根廷"},
	"AS": {"American Samoa", "美属萨摩亚"},
	"AT": {"Austria", "奥地利"},
	"AU": {"Australia", "澳大利亚"},
	"AW": {"Aruba", "阿鲁巴"},
	"AX": {"Aland Islands", "奥兰群岛"},
	"AZ": {"Azerbaijan", "阿塞拜疆"},
	"BA": {"Bosnia and Herzegovina", "波黑"},
	"BB": {"Barbados", "巴巴多斯"},
	"BD": {"Bangladesh", "孟加拉国"},
	"BE": {"Belgium", "比利时"},
	"BF": {"Burkina Faso", "布基纳法索"},
	"BG": {"Bulgaria", "保加利亚"},
	"BH": {"Bahrain", "巴林"},
	"BI": {"Burundi", "布隆迪"},
	"BJ": {"Benin", "贝宁"},
	"BL": {"Saint Barthelemy", "圣巴泰勒米"},
	"BM": {"Bermuda", "百慕大"},
	"BN": {"Brunei", "文莱"},
	"BO": {"Bolivia", "玻利维亚"},
	"BQ": {"Caribbean Netherlands", "荷兰加勒比区"},
	"BR": {"Brazil", "巴西"},
	"BS": {"Bahamas", "巴哈马"},
	"BT": {"Bhutan", "不丹"},
	"BV": {"Bouvet Island", "布韦岛"},
	"BW": {"Botswana", "博茨瓦纳"},
	"BY": {"Belarus", "白俄罗斯"},
	"BZ": {"Belize", "伯利兹"},
	"CA": {"Canada", "加拿大"},
	"CC": {"Cocos Islands", "科科斯群岛"},
	"CD": {"DR Congo", "刚果（金）"},
	"CF": {"Central African Republic", "中非"},
	"CG": {"Congo", "刚果（布）"},
	"CH": {"Switzerland", "瑞士"},
	"CI": {"Cote d'Ivoire", "科特迪瓦"},
	"CK": {"Cook Islands", "库克群岛"},
	"CL": {"Chile", "智利"},
	"CM": {"Cameroon", "喀麦隆"},
	"CN": {"China", "中国"},
	"CO": {"Colombia", "哥伦比亚"},
	"CR": {"Costa Rica", "哥斯达黎加"},
	"CU": {"Cuba", "古巴"},
	"CV": {"Cape Verde", "佛得角"},
	"CW": {"Curacao", "库拉索"},
	"CX": {"Christmas Island", "圣诞岛"},
	"CY": {"Cyprus", "塞浦路斯"},
	"CZ": {"Czechia", "捷克"},
	"DE": {"Germany", "德国"},
	"DJ": {"Djibouti", "吉布提"},
	"DK": {"Denmark", "丹麦"},
	"DM": {"Dominica", "多米尼克"},
	"DO": {"Dominican Republic", "多米尼加"},
	"DZ": {"Algeria", "阿尔及利亚"},
	"EC": {"Ecuador", "厄瓜多尔"},
	"EE": {"Estonia", "爱沙尼亚"},
	"EG": {"Egypt", "埃及"},
	"EH": {"Western Sahara", "西撒哈拉"},
	"ER": {"Eritrea", "厄立特里亚"},
	"ES": {"Spain", "西班牙"},
	"ET": {"Ethiopia", "埃塞俄比亚"},
	"FI": {"Finland", "芬兰"},
	"FJ": {"Fiji", "斐济"},
	"FK": {"Falkland Islands", "福克兰群岛"},
	"FM": {"Micronesia", "密克罗尼西亚"},
	"FO": {"Faroe Islands", "法罗群岛"},
	"FR": {"France", "法国"},
	"GA": {"Gabon", "加蓬"},
	"GB": {"United Kingdom", "英国"},
	"GD": {"Grenada", "格林纳达"},
	"GE": {"Georgia", "格鲁吉亚"},
	"GF": {"French Guiana", "法属圭亚那"},
	"GG": {"Guernsey", "根西岛"},
	"GH": {"Ghana", "加纳"},
	"GI": {"Gibraltar", "直布罗陀"},
	"GL": {"Greenland", "格陵兰"},
	"GM": {"Gambia", "冈比亚"},
	"GN": {"Guinea", "几内亚"},
	"GP": {"Guadeloupe", "瓜德罗普"},
	"GQ": {"Equatorial Guinea", "赤道几内亚"},
	"GR": {"Greece", "希腊"},
	"GS": {"South Georgia", "南乔治亚"},
	"GT": {"Guatemala", "危地马拉"},
	"GU": {"Guam", "关岛"},
	"GW": {"Guinea-Bissau", "几内亚比绍"},
	"GY": {"Guyana", "圭亚那"},
	"HK": {"Hong Kong", "香港"},
	"HM": {"Heard and McDonald Islands", "赫德岛和麦克唐纳群岛"},
	"HN": {"Honduras", "洪都拉斯"},
	"HR": {"Croatia", "克罗地亚"},
	"HT": {"Haiti", "海地"},
	"HU": {"Hungary", "匈牙利"},
	"ID": {"Indonesia", "印度尼西亚"},
	"IE": {"Ireland", "爱尔兰"},
	"IL": {"Israel", "以色列"},
	"IM": {"Isle of Man", "马恩岛"},
	"IN": {"India", "印度"},
	"IO": {"British Indian Ocean Territory", "英属印度洋领地"},
	"IQ": {"Iraq", "伊拉克"},
	"IR": {"Iran", "伊朗"},
	"IS": {"Iceland", "冰岛"},
	"IT": {"Italy", "意大利"},
	"JE": {"Jersey", "泽西岛"},
	"JM": {"Jamaica", "牙买加"},
	"JO": {"Jordan", "约旦"},
	"JP": {"Japan", "日本"},
	"KE": {"Kenya", "肯尼亚"},
	"KG": {"Kyrgyzstan", "吉尔吉斯斯坦"},
	"KH": {"Cambodia", "柬埔寨"},
	"KI": {"Kiribati", "基里巴斯"},
	"KM": {"Comoros", "科摩罗"},
	"KN": {"Saint Kitts and Nevis", "圣基茨和尼维斯"},
	"KP": {"North Korea", "朝鲜"},
	"KR": {"South Korea", "韩国"},
	"KW": {"Kuwait", "科威特"},
	"KY": {"Cayman Islands", "开曼群岛"},
	"KZ": {"Kazakhstan", "哈萨克斯坦"},
	"LA": {"Laos", "老挝"},
	"LB": {"Lebanon", "黎巴嫩"},
	"LC": {"Saint Lucia", "圣卢西亚"},
	"LI": {"Liechtenstein", "列支敦士登"},
	"LK": {"Sri Lanka", "斯里兰卡"},
	"LR": {"Liberia", "利比里亚"},
	"LS": {"Lesotho", "莱索托"},
	"LT": {"Lithuania", "立陶宛"},
	"LU": {"Luxembourg", "卢森堡"},
	"LV": {"Latvia", "拉脱维亚"},
	"LY": {"Libya", "利比亚"},
	"MA": {"Morocco", "摩洛哥"},
	"MC": {"Monaco", "摩纳哥"},
	"MD": {"Moldova", "摩尔多瓦"},
	"ME": {"Montenegro", "黑山"},
	"MF": {"Saint Martin", "法属圣马丁"},
	"MG": {"Madagascar", "马达加斯加"},
	"MH": {"Marshall Islands", "马绍尔群岛"},
	"MK": {"North Macedonia", "北马其顿"},
	"ML": {"Mali", "马里"},
	"MM": {"Myanmar", "缅甸"},
	"MN": {"Mongolia", "蒙古"},
	"MO": {"Macao", "澳门"},
	"MP": {"Northern Mariana Islands", "北马里亚纳群岛"},
	"MQ": {"Martinique", "马提尼克"},
	"MR": {"Mauritania", "毛里塔尼亚"},
	"MS": {"Montserrat", "蒙特塞拉特"},
	"MT": {"Malta", "马耳他"},
	"MU": {"Mauritius", "毛里求斯"},
	"MV": {"Maldives", "马尔代夫"},
	"MW": {"Malawi", "马拉维"},
	"MX": {"Mexico", "墨西哥"},
	"MY": {"Malaysia", "马来西亚"},
	"MZ": {"Mozambique", "莫桑比克"},
	"NA": {"Namibia", "纳米比亚"},
	"NC": {"New Caledonia", "新喀里多尼亚"},
	"NE": {"Niger", "尼日尔"},
	"NF": {"Norfolk Island", "诺福克岛"},
	"NG": {"Nigeria", "尼日利亚"},
	"NI": {"Nicaragua", "尼加拉瓜"},
	"NL": {"Netherlands", "荷兰"},
	"NO": {"Norway", "挪威"},
	"NP": {"Nepal", "尼泊尔"},
	"NR": {"Nauru", "瑙鲁"},
	"NU": {"Niue", "纽埃"},
	"NZ": {"New Zealand", "新西兰"},
	"OM": {"Oman", "阿曼"},
	"PA": {"Panama", "巴拿马"},
	"PE": {"Peru", "秘鲁"},
	"PF": {"French Polynesia", "法属波利尼西亚"},
	"PG": {"Papua New Guinea", "巴布亚新几内亚"},
	"PH": {"Philippines", "菲律宾"},
	"PK": {"Pakistan", "巴基斯坦"},
	"PL": {"Poland", "波兰"},
	"PM": {"Saint Pierre and Miquelon", "圣皮埃尔和密克隆"},
	"PN": {"Pitcairn Islands", "皮特凯恩群岛"},
	"PR": {"Puerto Rico", "波多黎各"},
	"PS": {"Palestine", "巴勒斯坦"},
	"PT": {"Portugal", "葡萄牙"},
	"PW": {"Palau", "帕劳"},
	"PY": {"Paraguay", "巴拉圭"},
	"QA": {"Qatar", "卡塔尔"},
	"RE": {"Reunion", "留尼汪"},
	"RO": {"Romania", "罗马尼亚"},
	"RS": {"Serbia", "塞尔维亚"},
	"RU": {"Russia", "俄罗斯"},
	"RW": {"Rwanda", "卢旺达"},
	"SA": {"Saudi Arabia", "沙特阿拉伯"},
	"SB": {"Solomon Islands", "所罗门群岛"},
	"SC": {"Seychelles", "塞舌尔"},
	"SD": {"Sudan", "苏丹"},
	"SE": {"Sweden", "瑞典"},
	"SG": {"Singapore", "新加坡"},
	"SH": {"Saint Helena", "圣赫勒拿"},
	"SI": {"Slovenia", "斯洛文尼亚"},
	"SJ": {"Svalbard and Jan Mayen", "斯瓦尔巴和扬马延"},
	"SK": {"Slovakia", "斯洛伐克"},
	"SL": {"Sierra Leone", "塞拉利昂"},
	"SM": {"San Marino", "圣马力诺"},
	"SN": {"Senegal", "塞内加尔"},
	"SO": {"Somalia", "索马里"},
	"SR": {"Suriname", "苏里南"},
	"SS": {"South Sudan", "南苏丹"},
	"ST": {"Sao Tome and Principe", "圣多美和普林西比"},
	"SV": {"El Salvador", "萨尔瓦多"},
	"SX": {"Sint Maarten", "荷属圣马丁"},
	"SY": {"Syria", "叙利亚"},
	"SZ": {"Eswatini", "斯威士兰"},
	"TC": {"Turks and Caicos Islands", "特克斯和凯科斯群岛"},
	"TD": {"Chad", "乍得"},
	"TF": {"French Southern Territories", "法属南部领地"},
	"TG": {"Togo", "多哥"},
	"TH": {"Thailand", "泰国"},
	"TJ": {"Tajikistan", "塔吉克斯坦"},
	"TK": {"Tokelau", "托克劳"},
	"TL": {"Timor-Leste", "东帝汶"},
	"TM": {"Turkmenistan", "土库曼斯坦"},
	"TN": {"Tunisia", "突尼斯"},
	"TO": {"Tonga", "汤加"},
	"TR": {"Turkey", "土耳其"},
	"TT": {"Trinidad and Tobago", "特立尼达和多巴哥"},
	"TV": {"Tuvalu", "图瓦卢"},
	"TW": {"Taiwan", "台湾"},
	"TZ": {"Tanzania", "坦桑尼亚"},
	"UA": {"Ukraine", "乌克兰"},
	"UG": {"Uganda", "乌干达"},
	"UM": {"U.S. Minor Outlying Islands", "美国本土外小岛屿"},
	"US": {"United States", "美国"},
	"UY": {"Uruguay", "乌拉圭"},
	"UZ": {"Uzbekistan", "乌兹别克斯坦"},
	"VA": {"Vatican City", "梵蒂冈"},
	"VC": {"Saint Vincent and the Grenadines", "圣文森特和格林纳丁斯"},
	"VE": {"Venezuela", "委内瑞拉"},
	"VG": {"British Virgin Islands", "英属维尔京群岛"},
	"VI": {"U.S. Virgin Islands", "美属维尔京群岛"},
	"VN": {"Vietnam", "越南"},
	"VU": {"Vanuatu", "瓦努阿图"},
	"WF": {"Wallis and Futuna", "瓦利斯和富图纳"},
	"WS": {"Samoa", "萨摩亚"},
	"YE": {"Yemen", "也门"},
	"YT": {"Mayotte", "马约特"},
	"ZA": {"South Africa", "南非"},
	"ZM": {"Zambia", "赞比亚"},
	"ZW": {"Zimbabwe", "津巴布韦"},
}

// countryAliases covers the labels that are not the table's English name: the
// ISO long forms VPN Gate publishes ("Korea Republic of"), older spellings, and
// the shorthands a user is likely to type. Keys are in the normalized form
// normalizeCountryName produces.
var countryAliases = map[string]string{
	"korea republic of":                         "KR",
	"republic of korea":                         "KR",
	"korea":                                     "KR",
	"south korea":                               "KR",
	"korea democratic people s republic of":     "KP",
	"russian federation":                        "RU",
	"viet nam":                                  "VN",
	"taiwan province of china":                  "TW",
	"chinese taipei":                            "TW",
	"hong kong sar":                             "HK",
	"macau":                                     "MO",
	"macao sar":                                 "MO",
	"iran islamic republic of":                  "IR",
	"moldova republic of":                       "MD",
	"republic of moldova":                       "MD",
	"bolivia plurinational state of":            "BO",
	"venezuela bolivarian republic of":          "VE",
	"tanzania united republic of":               "TZ",
	"united republic of tanzania":               "TZ",
	"syrian arab republic":                      "SY",
	"lao people s democratic republic":          "LA",
	"brunei darussalam":                         "BN",
	"czech republic":                            "CZ",
	"slovak republic":                           "SK",
	"macedonia the former yugoslav republic of": "MK",
	"macedonia":                                 "MK",
	"palestine state of":                        "PS",
	"palestinian territory":                     "PS",
	"congo the democratic republic of the":      "CD",
	"democratic republic of the congo":          "CD",
	"cote d ivoire":                             "CI",
	"ivory coast":                               "CI",
	"cabo verde":                                "CV",
	"swaziland":                                 "SZ",
	"burma":                                     "MM",
	"great britain":                             "GB",
	"england":                                   "GB",
	"uk":                                        "GB",
	"usa":                                       "US",
	"united states of america":                  "US",
	"uae":                                       "AE",
	"holy see":                                  "VA",
	"holy see vatican city state":               "VA",
	"vatican":                                   "VA",
	"saint martin french part":                  "MF",
	"sint maarten dutch part":                   "SX",
	"virgin islands british":                    "VG",
	"virgin islands u s":                        "VI",
	"falkland islands malvinas":                 "FK",
	"micronesia federated states of":            "FM",
	"cocos keeling islands":                     "CC",
	"turkiye":                                   "TR",
	"east timor":                                "TL",
	"aland islands":                             "AX",
	"south georgia and the south sandwich islands": "GS",
	"heard island and mcdonald islands":            "HM",
	"united states minor outlying islands":         "UM",
	"czechia":                                      "CZ",
	"南韩":                                           "KR",
	"北韩":                                           "KP",
	"俄罗斯联邦":                                        "RU",
	"美利坚合众国":                                       "US",
	"英格兰":                                          "GB",
	"越南社会主义共和国":                                    "VN",
}

// countryIndex resolves a normalized label — English name, Chinese name, or
// alias — to its alpha-2 code.
var countryIndex = buildCountryIndex()

func buildCountryIndex() map[string]string {
	index := make(map[string]string, len(countryTable)*2+len(countryAliases))
	for code, n := range countryTable {
		index[normalizeCountryName(n.english)] = code
		index[n.chinese] = code
	}
	for alias, code := range countryAliases {
		index[alias] = code
	}
	return index
}

// normalizeCountryName folds the punctuation the various sources disagree about
// ("Korea, Republic of" vs "Korea Republic of") into single spaces so one alias
// covers every spelling of the same label.
func normalizeCountryName(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch r {
		case ',', '.', '(', ')', '\'', '’', '-', '/', ' ', '\t':
			space = b.Len() > 0
		default:
			if space {
				b.WriteRune(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CountryCode resolves a code, an English label, or a Chinese name to its
// alpha-2 code, returning "" when the table carries no match.
func CountryCode(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if len(v) == 2 && isASCIILetters(v) {
		upper := strings.ToUpper(v)
		if _, ok := countryTable[upper]; ok {
			return upper
		}
	}
	return countryIndex[normalizeCountryName(v)]
}

// LookupCountry resolves a node's country code (preferred) or its provider
// label to the reference table.
func LookupCountry(code, name string) (Country, bool) {
	resolved := CountryCode(code)
	if resolved == "" {
		resolved = CountryCode(name)
	}
	if resolved == "" {
		return Country{}, false
	}
	n := countryTable[resolved]
	return Country{Code: resolved, English: n.english, Chinese: n.chinese}, true
}

// CountryChinese is the label the console shows for a node: the Chinese name
// when the country is known, otherwise whatever the provider called it.
func CountryChinese(code, name string) string {
	if c, ok := LookupCountry(code, name); ok {
		return c.Chinese
	}
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return strings.ToUpper(strings.TrimSpace(code))
}

// CountryFlag renders an alpha-2 code as its flag emoji — two regional
// indicator symbols. Codes outside the table get "" so the caller can decide
// what a missing flag looks like.
func CountryFlag(code string) string {
	resolved := CountryCode(code)
	if resolved == "" {
		return ""
	}
	const regionalIndicatorA = 0x1F1E6
	return string([]rune{
		rune(regionalIndicatorA + int(resolved[0]-'A')),
		rune(regionalIndicatorA + int(resolved[1]-'A')),
	})
}

// SameCountry reports whether a node belongs to the country the user pinned.
// The selection may be a code, an English name, or a Chinese name; all three
// resolve to the same code before comparing, so "日本", "Japan" and "JP" pin the
// same nodes.
func SameCountry(nodeName, nodeCode, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	if wantCode := CountryCode(want); wantCode != "" {
		if c, ok := LookupCountry(nodeCode, nodeName); ok {
			return c.Code == wantCode
		}
		return false
	}
	// A label the table does not carry can still be pinned literally, so a
	// country added by a future provider is not unreachable.
	return strings.EqualFold(strings.TrimSpace(nodeName), want) ||
		strings.EqualFold(strings.TrimSpace(nodeCode), want)
}

// MatchCountryCodes returns the codes a search term names. Chinese names match
// on any substring ("韩" finds 韩国), English names and aliases match on a word
// prefix ("uni" finds United States and United Kingdom, but "an" does not find
// Japan), and a bare two-letter term matches its code.
//
// The result is how a search for a country reaches nodes whose stored label is
// English: the caller turns it into a country_code test.
func MatchCountryCodes(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	lower := strings.ToLower(q)
	seen := map[string]bool{}
	var out []string
	add := func(code string) {
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	if len(q) == 2 && isASCIILetters(q) {
		if _, ok := countryTable[strings.ToUpper(q)]; ok {
			add(strings.ToUpper(q))
		}
	}
	englishOK := len(lower) >= 2 && isASCIIWord(lower)
	for code, n := range countryTable {
		if strings.Contains(n.chinese, q) {
			add(code)
			continue
		}
		if englishOK && hasWordPrefix(normalizeCountryName(n.english), lower) {
			add(code)
		}
	}
	if englishOK {
		for alias, code := range countryAliases {
			if hasWordPrefix(alias, lower) {
				add(code)
			}
		}
	}
	sort.Strings(out)
	return out
}

// hasWordPrefix reports whether any space-separated word of a normalized name
// starts with the prefix.
func hasWordPrefix(name, prefix string) bool {
	for _, word := range strings.Split(name, " ") {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return false
}

func isASCIILetters(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

// isASCIIWord keeps a Chinese term from being tried against the English names,
// where it can never match but would cost a scan of the table.
func isASCIIWord(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
