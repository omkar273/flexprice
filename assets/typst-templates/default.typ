#let parse-date = (date-str) => {
  let parts = date-str.split("-")
  if parts.len() != 3 {
    panic(
      "Invalid date string: " + date-str + "\n" +
      "Expected format: YYYY-MM-DD"
    )
  }
  datetime(
    year: int(parts.at(0)),
    month: int(parts.at(1)),
    day: int(parts.at(2)),
  )
}

#let format-date = (date) => {
  let month-names = (
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"
  )

  let day = if date.day() < 10 {
    "0" + str(date.day())
  } else {
    str(date.day())
  }
  let month = month-names.at(date.month() - 1)
  let year = str(date.year()).slice(2) // Get last 2 digits

  day + " " + month + " " + year
}


#let format-number = (num, precision: 2) => {
  let str-num = str(num)
  let parts = str-num.split(".")
  let integer-part = str(parts.at(0))
  let decimal-part = if parts.len() > 1 { 
    let raw-decimal = parts.at(1)
    // Ensure exactly the specified precision decimal places
    if raw-decimal.len() < precision {
      let zeros = ""
      for i in range(precision - raw-decimal.len()) {
        zeros += "0"
      }
      raw-decimal + zeros
    } else if raw-decimal.len() > precision {
      raw-decimal.slice(0, precision)
    } else {
      raw-decimal
    }
  } else { 
    let zeros = ""
    for i in range(precision) {
      zeros += "0"
    }
    zeros
  }

  // Add commas every 3 digits from the right
  let chars = integer-part.rev().clusters()
  let result = ""
  for (i, c) in chars.enumerate() {
    if calc.rem-euclid(i, 3) == 0 and i != 0 {
      result += ","
    }
    result += c
  }

  if precision > 0 {
    result.rev() + "." + decimal-part
  } else {
    result.rev()
  }
}

#let format-currency = (num, precision: 2) => {
  let multiplier = calc.pow(10.0, precision)
  let rounded = calc.round(num * multiplier) / multiplier
  format-number(rounded, precision: precision)
}

// Define the default-invoice function
#let default-invoice(
  language: "en",
  currency: "$",
  precision: 2,
  title: none,
  banner-image: none,
  invoice-status: "DRAFT",       // DRAFT, FINALIZED, VOIDED
  invoice-number: none,
  issuing-date: "",
  due-date: none,
  amount-due: 0,  
  notes: "",
  biller: (:),                  // Company info
  recipient: (:),               // Customer info
  keywords: (),
  styling: (:),                 // font, font-size, margin (sets defaults below)
  items: (),                    // Line items
  applied-taxes: (),            // Applied taxes breakdown
  applied-discounts: (),        // Applied discounts breakdown
  subtotal: 0,                  // Subtotal before discounts and tax
  discount: 0,                  // Total discounts
  tax: 0,                       // Total tax
  doc,
) = {
  // Set styling defaults
  styling.font = styling.at("font", default: "Inter")
  styling.font-size = styling.at("font-size", default: 9pt)
  styling.primary-color = styling.at("primary-color", default: rgb("#000000"))
  styling.margin = styling.at("margin", default: (
    top: 15mm,
    right: 15mm,
    bottom: 15mm,
    left: 15mm,
  ))
  styling.line-color = styling.at("line-color", default: rgb("#e0e0e0"))
  styling.secondary-color = rgb(styling.at("secondary-color", default: rgb("#707070")))
  styling.table-header-bg = rgb("#f7f7f7")
  styling.table-header-color = rgb("#000000")

  // Set document properties
  let issuing-date-value = if issuing-date != "" { issuing-date }
        else { datetime.today().display("[year]-[month]-[day]") }

  set document(
    title: if title != none { title } else { "Invoice " + invoice-number },
    keywords: keywords,
    date: parse-date(issuing-date-value),
  )

  set page(
    margin: styling.margin,
    numbering: none,
  )

  set text(
    font: styling.font,
    size: styling.font-size,
  )

  set table(stroke: none)

  // Document header with banner image if provided
  if banner-image != none {
    grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      gutter: 0em,
      [
        #banner-image
      ],
      [
        #text(weight: "medium", size: 1.6em)[Invoice]
      ]
    )
    v(0.8em)
  } else {
    text(weight: "medium", size: 1.6em, fill: styling.primary-color)[Invoice]
  }

  grid(
    columns: (1fr, 1fr, 1fr),
    gutter: 0.5em,
    align: auto,
    [
      #text(weight: "regular", fill: styling.secondary-color)[Invoice Number]\
      #text(weight: "regular")[#invoice-number]
    ],
    [
      #text(weight: "regular", fill: styling.secondary-color)[Date of Issue]\
      #text(weight: "regular")[#issuing-date-value]
    ],
    [
      #text(weight: "regular", fill: styling.secondary-color)[Date Due]\
      #text(weight: "regular")[#due-date]
    ],
  )

  line(length: 100%, stroke: 0.5pt + styling.line-color)

  v(2em)

  // Biller and Recipient Information
  grid(
    columns: (1fr, 1fr),
    gutter: 1em,
    [
      #text(weight: "semibold", size: 12pt)[From]
      #v(0.25em)
      #text(weight: "medium")[#biller.name] \
      #text(fill: gray)[#biller.at("email", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("street", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("city", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("postal-code", default: "--")]
    ],
    [
      #text(weight: "semibold", size: 12pt)[Bill to]
      #v(0.25em)
      #text(weight: "medium")[#recipient.name] \
      #text(fill: gray)[#recipient.at("email", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("street", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("city", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("postal-code", default: "--")]
    ]
  )

  v(2em)
  line(length: 100%, stroke: 0.5pt + styling.line-color)
  v(1em)

  // Order Details
  text(weight: "medium", size: 1.1em)[Order Details]
  v(0.8em)

  // Main line items table with integrated usage breakdowns
  for (i, item) in items.enumerate() {
    // Amount is already the total line amount, not unit price
    let line-total = item.amount
    let amount-display = if line-total < 0 {
      [−#currency #format-currency(calc.abs(line-total), precision: precision)]
    } else {
      [#currency #format-currency(line-total, precision: precision)]
    }
    
    let description = if item.at("description", default: "Recurring") != "" {
      item.at("description", default: "Recurring")
    } else {
      "-"
    }
    
    let has_period = item.at("period_start", default: "") != "" and item.at("period_end", default: "") != ""
    let interval = if has_period {
      [#format-date(parse-date(item.at("period_start"))) - #format-date(parse-date(item.at("period_end")))]
    } else {
      "-"
    }
    
    // Display the main line item
    if i == 0 {
      // Add header only for the first item
      table(
        columns: (1fr, 2fr, 1fr, 1fr, 1fr),
        inset: 7pt,
        align: (left, left, left, center, right),
        fill: (x, y) => if y == 0 { styling.table-header-bg } else { white },
        stroke: (x, y) => (
          bottom: 0.5pt + styling.line-color,
        ),
      table.header(
        [#text(fill: styling.table-header-color, weight: "medium")[Item]],
        [#text(fill: styling.table-header-color, weight: "medium")[Description]],
        [#text(fill: styling.table-header-color, weight: "medium")[Interval]],
        [#text(fill: styling.table-header-color, weight: "medium")[Quantity]],
        [#text(fill: styling.table-header-color, weight: "medium")[Amount]],
      ),
        [#item.at("plan_display_name", default: "Plan")], 
        [#description],
        [#interval],
        [#format-number(item.quantity)],
        [#amount-display]
      )
    } else {
      // Just the row for subsequent items
      table(
        columns: (1fr, 2fr, 1fr, 1fr, 1fr),
        inset: 7pt,
        align: (left, left, left, center, right),
        fill: (x, y) => if y == 0 { styling.table-header-bg } else { white },
        stroke: (x, y) => (
          bottom: 0.5pt + styling.line-color,
        ),
        [#item.at("plan_display_name", default: "Plan")], 
        [#description],
        [#interval],
        [#format-number(item.quantity)],
        [#amount-display]
      )
    }
    
    // Check if this item has usage breakdown and add it directly below
    if "usage_breakdown" in item and item.usage_breakdown != none and item.usage_breakdown.len() > 0 {
      v(0.3em)
      
      // Create a simplified usage breakdown table
      for usage_item in item.usage_breakdown {
        // Check if grouped_by exists
        if "grouped_by" in usage_item {
          // Extract data from the usage breakdown item
          let grouped_by = usage_item.grouped_by
          let cost = usage_item.cost
          let usage = usage_item.at("usage", default: none)
          
          // Extract resource name from grouped_by map
          let resource_name = grouped_by.at("resource_name", default: "-")
          
          // Parse cost string to float safely
          let cost_value = 0.0
          if cost != none {
            cost_value = float(str(cost))
          }
          
          // Parse usage/units safely
          let usage_value = 0.0
          if usage != none {
            usage_value = float(str(usage))
          }
          
          // Display usage row with resource name in first column, blank for description and interval, 
          // then quantity and amount aligned with main table
          table(
            columns: (1fr, 2fr, 1fr, 1fr, 1fr),
            inset: (left: 1.5em, rest: 6pt),
            align: (left, left, left, center, right),
            fill: rgb("#fafafa"),
            stroke: none,
            [#text(size: 0.85em, fill: styling.secondary-color)[#resource_name]],
            [],  // Empty description column
            [],  // Empty interval column
            [#text(size: 0.85em)[#format-number(usage_value)]],
            [#text(size: 0.85em)[#currency #format-currency(cost_value, precision: precision)]]
          )
        }
      }
    }
    
    v(0.5em)
  }
  
  // End of line items with usage breakdowns

  v(1em)

  // Totals
  align(right,
    table(
      columns: 2,
      align: (left, right),
      inset: 6pt,
      stroke: none,
      // Always show subtotal
      [Subtotal], [#currency#format-currency(subtotal, precision: precision)],
      
      // Show discount row only if there's a discount
      ..if discount > 0 { ([Discount], [−#currency#format-currency(discount, precision: precision)]) } else { () },
      
      // Show tax row only if there's tax
      ..if tax > 0 { ([Tax], [#currency#format-currency(tax, precision: precision)]) } else { () },
      
      table.hline(stroke: 0.5pt + black),
      [*Net Payable*], [*#currency#format-currency(subtotal - discount + tax, precision: precision)*],
    )
  )

  v(2em)

  // Applied Discounts section (if any discounts were applied)
  if applied-discounts.len() > 0 {
    text(weight: "medium", size: 1.1em)[Applied Discounts]
    v(0.5em)

    table(
      columns: (1fr, 1fr, 1fr, 1fr, 1fr),
      inset: 8pt,
      align: (left, left, right, right, left),
      fill: white,
      stroke: (x, y) => (
        bottom: if y == 0 { 1pt + styling.line-color } else { 1pt + styling.line-color },
      ),
      table.header(
        [*Discount Name*],
        [*Type*],
        [*Value*],
        [*Discount Amount*],
        [*Line Item Ref.*],
      ),
      ..applied-discounts.map((discount) => {
        let value-display = if discount.type == "percentage" {
          [#format-currency(discount.value, precision: precision)%]
        } else {
          [#currency#format-currency(discount.value, precision: precision)]
        }
        
        (
          discount.discount_name,
          discount.type,
          value-display,
          [#currency#format-currency(discount.discount_amount, precision: precision)],
          discount.line_item_ref,
        )
      }).flatten(),
    )

    v(1em)
  }

  // Applied Taxes section (if any taxes were applied)
  if applied-taxes.len() > 0 {
    text(weight: "medium", size: 1.1em)[Applied Taxes]
    v(0.5em)

    table(
      columns: (1fr, 1fr, 1fr, 1fr, 1fr, 1fr),
      inset: 8pt,
      align: (left, left, left, right, right, right),
      fill: white,
      stroke: (x, y) => (
        bottom: if y == 0 { 1pt + styling.line-color } else { 1pt + styling.line-color },
      ),
      table.header(
        [*Tax Name*],
        [*Code*],
        [*Type*],
        [*Rate*],
        [*Taxable Amount*],
        [*Tax Amount*],
        // [*Applied At*],
      ),
      ..applied-taxes.map((tax) => {
        let rate-display = if tax.tax_type == "percentage" {
          [#format-currency(tax.tax_rate, precision: precision)%]
        } else {
          [#currency#format-currency(tax.tax_rate, precision: precision)]
        }
        
        (
          tax.tax_name,
          tax.tax_code,
          tax.tax_type,
          rate-display,
          [#currency#format-currency(tax.taxable_amount, precision: precision)],
          [#currency#format-currency(tax.tax_amount, precision: precision)],
          // tax.applied_at,
        )
      }).flatten(),
    )

    v(1em)
  }

  // Payment information
  if invoice-status == "FINALIZED" {
    text(weight: "medium", size: 1.1em)[Payment Information]
    v(0.8em)

    [We kindly request that you complete the payment by the due date of #due-date. Your prompt attention to this matter is greatly appreciated.]

    if "payment-instructions" in biller {
      v(0.5em)
      biller.payment-instructions
    }
  }

  // Notes
  if notes != "" {
    v(1em)
    text(weight: "medium", size: 1.1em)[Notes]
    v(0.5em)
    notes
  }

  // Footer
  v(3em)
  align(bottom,   align(center, text(size: 8pt)[
    #biller.name ⋅ 
    #{if "website" in biller {[#link("https://" + biller.website)[#biller.website] ⋅ ]}}
    #{if "help-email" in biller {[#link(biller.help-email)[#biller.help-email]]}}
  ]))

  doc
}