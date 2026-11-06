??? info "Attribution flags"

    Accepted by the four mutating commands (`message send`, `message edit`,
    `reply`, `file upload`):

    | Flag | Value | Description |
    |------|-------|-------------|
    | `--[no-]attribution` | | Toggle attribution for this command |
    | `--attribution-label` | `<LABEL>` | Override attribution label |
    | `--attribution-emoji` | `<EMOJI>` | Override attribution emoji |
    | `--attribution-message` | `<TEXT>` | Override attribution message |

    A custom label appears in the generated caption, including manual runs:
    `--attribution --attribution-label "Release helper"` produces
    `Sent via slick (Release helper)`. An explicit `--attribution-message`
    supplies the caption instead. Emoji overrides change its leading emoji.

    The attribution toggles are bare switches. Pass `--attribution` or
    `--no-attribution`; explicit forms such as `--attribution=false` are rejected.
    The two switches are mutually exclusive. Presentation overrides do not
    enable attribution by themselves.
