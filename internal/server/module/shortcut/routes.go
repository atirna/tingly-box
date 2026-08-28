package shortcut

import "github.com/tingly-dev/tingly-box/swagger"

// RegisterRoutes wires the /shortcut endpoints onto an authenticated route
// group.
func RegisterRoutes(apiV1 *swagger.RouteGroup, h *Handler) {
	apiV1.GET("/shortcut", h.Status,
		swagger.WithTags("shortcut"),
		swagger.WithDescription("Check whether a desktop / start-menu shortcut already exists"),
		swagger.WithResponseModel(ShortcutStatusResponse{}),
	)

	apiV1.POST("/shortcut", h.Create,
		swagger.WithTags("shortcut"),
		swagger.WithDescription("Create (or refresh) a desktop / start-menu shortcut that launches Tingly Box"),
		swagger.WithRequestModel(ShortcutCreateRequest{}),
		swagger.WithResponseModel(ShortcutCreateResponse{}),
	)
}
