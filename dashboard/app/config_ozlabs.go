// OzLabs test configuration

package dash

func init() {
	installConfig(&GlobalConfig{
		AccessLevel:         AccessPublic,
		AuthDomain:          "@ozlabs.au.ibm.com",
		AnalyticsTrackingID: "",
		CoverPath:           "TODO-NOT-WORKING", // TODO: Put cover somewhere
		Namespaces: map[string]*Config{
			"linuxppc": {
				AccessLevel:      AccessPublic,
				DisplayTitle:     "Linux on PowerPC",
				SimilarityDomain: "1",
				Clients: map[string]string{
					"upstream-p8": "aaaabbbbccccdddd",
				},
				Key:               "aaaabbbbccccdddd",
				MailWithoutReport: false,
				// TODO: Current setup does not have outbound email.
				// Must restrict email to mailing our local list only.
				Reporting: []Reporting{
					{
						AccessLevel:  AccessPublic,
						Name:         "email",
						DisplayTitle: "syzbot-ozlabs",
						Config: &EmailConfig{
							Email:           "syzkaller@ozlabs.au.ibm.com",
							MailMaintainers: false,
						},
					},
				},
				Repos: []KernelRepo{
					{
						URL:               "git://gitlab.ozlabs.ibm.com/mirror/linux.git",
						Branch:            "master",
						Alias:             "mainline",
						ReportingPriority: 0,
					},
					{
						URL:               "git://gitlab.ozlabs.ibm.com/mirror/linux-next.git",
						Branch:            "master",
						Alias:             "linux-next",
						ReportingPriority: 1,
					},
				},
			},
		},
		Clients: map[string]string{},
	})
}
