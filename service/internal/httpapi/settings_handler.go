package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/qiuxsgit/qiuxs-blog/service/internal/settings"
)

func (h *AdminHandler) GetSiteSettings(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	site, err := h.site.GetSite(c.Request.Context())
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, siteSettingsView(site))
}

func (h *AdminHandler) PutSiteSettings(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	request, err := decodeAdminJSON[PutSiteSettingsRequest](c, c.Request, c.Writer, maxAdminMarkdownBodyBytes)
	if err != nil || request.LockVersion < 0 {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	socialLinks := make([]settings.SocialLink, len(request.SocialLinks))
	for index, item := range request.SocialLinks {
		socialLinks[index] = settings.SocialLink{Label: item.Label, URL: item.Url}
	}
	stored, err := h.site.PutSite(c.Request.Context(), settings.Site{
		SiteName: request.SiteName, AuthorName: request.AuthorName, AuthorBio: request.AuthorBio,
		HomeStatus: request.HomeStatus, AboutMD: request.AboutMd, SocialLinks: socialLinks,
		SEODefaultTitle: request.SeoDefaultTitle, SEODefaultDescription: request.SeoDefaultDescription,
		SEODefaultImageMediaID: copyInt64Pointer(request.SeoDefaultImageMediaId),
		FilingName:             request.FilingName, FilingNumber: request.FilingNumber,
	}, request.LockVersion)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, siteSettingsView(stored))
}

func (h *AdminHandler) GetHotlinkSettings(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	policy, err := h.hotlink.Get(c.Request.Context())
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, hotlinkSettingsView(policy))
}

func (h *AdminHandler) PutHotlinkSettings(c *gin.Context) {
	if !h.authenticate(c) {
		return
	}
	request, err := decodeAdminJSON[PutHotlinkSettingsRequest](c, c.Request, c.Writer, maxAdminJSONBodyBytes)
	if err != nil {
		WriteProblem(c, ErrInvalidRequest)
		return
	}
	entries := make([]settings.HotlinkEntry, len(request.Entries))
	for index, item := range request.Entries {
		entries[index] = settings.HotlinkEntry{Hostname: item.Hostname, Enabled: item.Enabled}
	}
	stored, err := h.hotlink.Put(c.Request.Context(), request.AllowEmptyReferer, entries)
	if err != nil {
		writeStage2Problem(c, err)
		return
	}
	c.JSON(http.StatusOK, hotlinkSettingsView(stored))
}

func siteSettingsView(site settings.Site) SiteSettingsView {
	socialLinks := make([]SocialLink, len(site.SocialLinks))
	for index, item := range site.SocialLinks {
		socialLinks[index] = SocialLink{Label: item.Label, Url: item.URL}
	}
	filingURL := settings.FilingURL
	var id *int64
	if site.ID > 0 {
		id = copyInt64Pointer(&site.ID)
	}
	var updatedAt = timePointerIfSet(site.UpdatedAt)
	return SiteSettingsView{
		Id: id, LockVersion: site.LockVersion, SiteName: site.SiteName, AuthorName: site.AuthorName,
		AuthorBio: site.AuthorBio, HomeStatus: site.HomeStatus, AboutMd: site.AboutMD, SocialLinks: socialLinks,
		SeoDefaultTitle: site.SEODefaultTitle, SeoDefaultDescription: site.SEODefaultDescription,
		SeoDefaultImageMediaId: copyInt64Pointer(site.SEODefaultImageMediaID), FilingName: site.FilingName,
		FilingNumber: site.FilingNumber, FilingUrl: &filingURL, UpdatedAt: updatedAt,
	}
}

func hotlinkSettingsView(policy settings.HotlinkPolicy) HotlinkSettingsView {
	entries := make([]HotlinkEntry, len(policy.Entries))
	for index, item := range policy.Entries {
		entries[index] = HotlinkEntry{Hostname: item.Hostname, Enabled: item.Enabled}
	}
	return HotlinkSettingsView{AllowEmptyReferer: policy.AllowEmptyReferer, Entries: entries}
}

func timePointerIfSet(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
