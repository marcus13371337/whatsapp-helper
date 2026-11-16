package wa

import "github.com/marcus13371337/whatsapp-helper/internal/helper"

var errBadCursor = helper.Errorf(helper.CodeBadRequest, "That `before` did not come from an earlier answer.")
