# Design System Document: Technical Sophistication & Editorial Precision

## 1. Overview & Creative North Star: "The Obsidian Architect"
The design system is built for the high-stakes environment of API management and data proxying. It rejects the "utility-only" look of standard dashboards in favor of **The Obsidian Architect**—a creative North Star that treats technical data as a premium, curated gallery. 

This system moves beyond basic data visualization by employing intentional asymmetry, overlapping technical layers, and a high-contrast typographic scale. We are not just building a tool; we are building a command center that feels authoritative, cinematic, and meticulously engineered. By utilizing deep charcoal foundations and warm metallic accents, we create a sense of "digital luxury" for the developer experience.

---

## 2. Colors: Tonal Depth & The "No-Line" Rule
The palette is rooted in the depth of `#131318` (Surface), punctuated by the warmth of Amber/Copper and the cooling precision of Muted Teal.

### The "No-Line" Rule
Standard 1px solid borders are strictly prohibited for structural sectioning. In this system, boundaries are defined through **Background Color Shifts**. 
*   **Example:** A `surface-container-low` component should sit on a `surface` background to define its edges naturally. 
*   **The "Ghost Border" Fallback:** If a border is required for high-density accessibility, use the `outline-variant` (`#53443b`) at 15% opacity. Never use 100% opaque borders.

### Surface Hierarchy & Nesting
Treat the UI as physical layers of obsidian and glass.
*   **Surface Lowest (#0e0e13):** Background for deep-nesting or "sunken" code blocks.
*   **Surface (#131318):** The primary canvas.
*   **Surface Container (#201f25):** The standard "card" or section lift.
*   **Surface Bright (#3a383f):** Reserved for hover states or active technical selections.

### The "Glass & Gradient" Rule
To elevate the aesthetic, use **Glassmorphism** for floating overlays (e.g., Modals or Dropdowns).
*   **Recipe:** `surface-container-highest` at 70% opacity + 20px Backdrop Blur.
*   **Signature Textures:** Use a subtle linear gradient for Primary CTAs: `primary` (`#ffb687`) to `primary-container` (`#d98f5d`) at a 135° angle. This adds "visual soul" to the interaction.

---

## 3. Typography: The Editorial Technical Scale
We utilize a triad of typefaces to separate intent: **Manrope** for impact, **Inter** for utility, and **Space Grotesk** for technical labeling.

*   **Display & Headlines (Manrope):** High-end, wide apertures. Used for page titles and high-level metrics. It transforms data into a headline story.
*   **Title & Body (Inter):** The workhorse. Inter provides maximum legibility for API endpoints, logs, and documentation.
*   **Labels (Space Grotesk):** This monospace-adjacent sans-serif is used for metadata, status tags, and technical micro-copy. It reinforces the "technical, data-driven" aesthetic.

---

## 4. Elevation & Depth: Tonal Layering
Hierarchy is achieved through **Tonal Layering** rather than structural lines.

*   **The Layering Principle:** Depth is "stacked." Place a `surface-container-highest` card on top of a `surface-container` section. The subtle contrast creates a natural lift.
*   **Ambient Shadows:** Floating elements (like Tooltips) must use an extra-diffused shadow: `offset-y: 12px, blur: 24px, color: rgba(0, 0, 0, 0.4)`. Avoid dark grey "drop shadows"; the shadow should feel like a natural absence of light.
*   **Glassmorphism Depth:** When using glass containers, ensure the `outline-variant` is used as a 1px "highlight" on the top and left edges only, mimicking light hitting the edge of a glass pane.

---

## 5. Components: Precision Primitives

### Buttons
*   **Primary:** Copper gradient (`primary` to `primary_container`) with `on-primary` text. 8px (`lg`) rounded corners.
*   **Secondary:** Ghost style. No background, `outline` border at 20% opacity. Text in `primary`.
*   **Tertiary:** Muted teal (`secondary`) text only. Used for low-priority actions in high-density areas.

### Cards & Data Lists
*   **Card Separation:** Strictly forbid divider lines. Use `0.75rem` (xl) vertical white space or a background shift from `surface-container` to `surface-container-low` to separate items.
*   **Technical Data Chips:** Use `secondary_container` background with `on_secondary_container` text. These should be 4px (`md`) rounded for a more "utilitarian" feel compared to buttons.

### Input Fields
*   **Standard State:** `surface-container-highest` background, no border, 8px corner.
*   **Active/Focus State:** A 1px "Ghost Border" using the `primary` amber color at 40% opacity.
*   **Monospace Code Inputs:** All API-specific inputs (Keys, Proxies) must use the Monospace scale for character alignment precision.

### Precision Components (App Specific)
*   **The "Proxy Pulse":** A small, animated glow using `secondary` (teal) to indicate active data flow, utilizing a soft blur effect rather than a solid circle.
*   **Log Streams:** High-density text blocks using `surface-container-lowest` backgrounds to create a "terminal" feel within the editorial layout.

---

## 6. Do’s and Don’ts

### Do:
*   **Use Asymmetry:** Place high-level metrics off-center or in varying card widths to break the "Bootstrap" grid feel.
*   **Embrace High Density:** This system is for pros. It's okay to have a lot of data on screen, provided the **Typography Scale** and **Tonal Layering** are strictly followed to maintain hierarchy.
*   **Use Subtle Micro-interactions:** Buttons should "glow" slightly on hover using a low-spread shadow of their own accent color.

### Don’t:
*   **Don't Use Pure White:** `#ffffff` is too harsh. Always use `on-background` (`#e5e1e9`) for text to maintain the premium dark-mode comfort.
*   **Don't Use 1px Dividers:** They clutter the technical interface. Trust the spacing and background shifts.
*   **Don't Round Everything:** Stick to the `8px` (`lg`) scale for cards/buttons, but use `0px` or `2px` for technical charts to maintain a "sharp" engineering edge.