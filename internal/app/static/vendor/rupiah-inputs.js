(function () {
  "use strict";

  // Compare decimal strings rather than coercing to Number: BIGINT Rupiah
  // values can exceed JavaScript's exact integer range.
  var maximumRupiahAmount = "9223372036854775807";

  function rupiahDigits(value) {
    var normalized = (value || "").trim().replace(/^rp\s*/i, "");
    if (/^\d+$/.test(normalized)) {
      return normalized;
    }
    if (/^\d{1,3}([.,]\d{3})+$/.test(normalized) && !(normalized.includes(".") && normalized.includes(","))) {
      return normalized.replace(/[.,]/g, "");
    }
    return null;
  }

  function canonicalDigits(digits) {
    return digits.replace(/^0+(?=\d)/, "");
  }

  function greaterThan(left, right) {
    left = canonicalDigits(left);
    right = canonicalDigits(right);
    return left.length > right.length || (left.length === right.length && left > right);
  }

  function errorElement(field) {
    var id = field.getAttribute("aria-errormessage");
    return id ? document.getElementById(id) : null;
  }

  function setError(field, message) {
    var error = errorElement(field);
    field.setCustomValidity(message || "");
    if (message) {
      field.setAttribute("aria-invalid", "true");
    } else {
      field.removeAttribute("aria-invalid");
    }
    if (error) {
      error.textContent = message || "";
      error.hidden = !message;
    }
  }

  function validateRupiah(field) {
    var digits = rupiahDigits(field.value);
    if (digits === null || !/[1-9]/.test(digits)) {
      setError(field, field.dataset.rupiahInvalid);
      return null;
    }
    digits = canonicalDigits(digits);
    var maximum = maximumRupiahAmount;
    var maximumMessage = field.dataset.rupiahInvalid;
    var workflowMaximum = rupiahDigits(field.dataset.rupiahMax || "");
    if (workflowMaximum !== null && !greaterThan(workflowMaximum, maximumRupiahAmount)) {
      maximum = workflowMaximum;
      maximumMessage = field.dataset.rupiahMaxInvalid || maximumMessage;
    }
    if (greaterThan(digits, maximum)) {
      setError(field, maximumMessage);
      return null;
    }
    setError(field, "");
    return digits;
  }

  function formatRupiah(field) {
    if (!field.value.trim()) {
      setError(field, "");
      return;
    }
    var digits = rupiahDigits(field.value);
    if (digits === null || !/[1-9]/.test(digits)) {
      setError(field, field.dataset.rupiahInvalid);
      return;
    }
    digits = canonicalDigits(digits);
    var group = field.dataset.rupiahGroup || ",";
    field.value = "Rp " + digits.replace(/\B(?=(\d{3})+(?!\d))/g, group);
    validateRupiah(field);
  }

  function formatRupiahDigits(digits, group) {
    return "Rp " + String(digits).replace(/\B(?=(\d{3})+(?!\d))/g, group || ",");
  }

  function roundHalfUp(numerator, denominator) {
    return (numerator + (denominator / 2n)) / denominator;
  }

  function adminFeeFor(type, amount, duration) {
    if (type === "regular") {
      var threshold = 25000000n;
      var firstTier = amount > threshold ? threshold : amount;
      var excessTier = amount > threshold ? amount - threshold : 0n;
      var monthly = roundHalfUp(firstTier * 10n + excessTier * 15n, 1000n);
      return { monthly: monthly, total: monthly * BigInt(duration || 1) };
    }
    if (type === "secondary_goods") {
      var secondary = roundHalfUp(amount * 20n, 100n);
      return { monthly: null, total: secondary };
    }
    if (type === "goods_purchase_paylater") {
      var paylater = roundHalfUp(amount * 5n, 100n);
      return { monthly: null, total: paylater };
    }
    return null;
  }

  function formLoanType(form) {
    var field = form.querySelector("[data-loan-type]");
    return field ? field.value : form.dataset.loanTypeValue;
  }

  function loanTypeMaxDuration(type) {
    if (type === "goods_purchase_paylater") {
      return 1;
    }
    if (type === "secondary_goods") {
      return 12;
    }
    return 24;
  }

  function updateLoanFeePreview(form) {
    var output = form.querySelector("[data-loan-fee-output]");
    var amountField = form.querySelector("[data-loan-amount]");
    var durationField = form.querySelector("[data-loan-duration]");
    if (!output || !amountField || !durationField) {
      return;
    }
    var type = formLoanType(form);
    var maxDuration = loanTypeMaxDuration(type);
    durationField.max = String(maxDuration);
    if (type === "goods_purchase_paylater") {
      durationField.value = "1";
    } else if (Number(durationField.value || "0") > maxDuration) {
      durationField.value = String(maxDuration);
    }
    var digits = rupiahDigits(amountField.value);
    var duration = Number(durationField.value || "0");
    if (!type || digits === null || !/[1-9]/.test(digits) || duration < 1 || duration > maxDuration) {
      output.hidden = true;
      output.textContent = "";
      return;
    }
    var amount = BigInt(canonicalDigits(digits));
    var fee = adminFeeFor(type, amount, duration);
    if (!fee) {
      output.hidden = true;
      output.textContent = "";
      return;
    }
    var group = amountField.dataset.rupiahGroup || form.dataset.rupiahGroup || ",";
    var totalObligation = amount + fee.total;
    var parts = [];
    if (fee.monthly !== null) {
      parts.push((form.dataset.monthlyAdminFeeLabel || "Monthly admin fee") + ": " + formatRupiahDigits(fee.monthly, group));
    }
    parts.push((form.dataset.totalAdminFeeLabel || "Total admin fee") + ": " + formatRupiahDigits(fee.total, group));
    parts.push((form.dataset.totalObligationLabel || "Total obligation") + ": " + formatRupiahDigits(totalObligation, group));
    output.textContent = parts.join(" · ");
    output.hidden = false;
  }

  function enhanceLoanFeePreview(form) {
    if (form.dataset.loanFeePreviewEnhanced) {
      return;
    }
    form.dataset.loanFeePreviewEnhanced = "true";
    ["input", "change"].forEach(function (eventName) {
      form.addEventListener(eventName, function () { updateLoanFeePreview(form); });
    });
    updateLoanFeePreview(form);
  }

  function enhanceLoanFeePreviews(root) {
    (root || document).querySelectorAll("[data-loan-fee-preview]").forEach(enhanceLoanFeePreview);
  }

  function enhanceRupiahInput(field) {
    if (field.dataset.rupiahEnhanced) {
      return;
    }
    field.dataset.rupiahEnhanced = "true";
    field.addEventListener("input", function () { formatRupiah(field); });
    field.addEventListener("blur", function () { formatRupiah(field); });
    if (field.value) {
      formatRupiah(field);
    }
  }

  function enhanceRupiahInputs(root) {
    (root || document).querySelectorAll("[data-rupiah-input]").forEach(enhanceRupiahInput);
  }

  function rupiahFields(element) {
    if (!element) {
      return [];
    }
    if (element.matches && element.matches("[data-rupiah-input]")) {
      return [element];
    }
    return element.querySelectorAll ? element.querySelectorAll("[data-rupiah-input]") : [];
  }

  function normalizeParameters(parameters, form) {
    rupiahFields(form).forEach(function (field) {
      var digits = validateRupiah(field);
      if (digits === null || !field.name) {
        return;
      }
      if (parameters && typeof parameters.set === "function") {
        parameters.set(field.name, digits);
      } else if (parameters) {
        parameters[field.name] = digits;
      }
    });
  }

  document.addEventListener("submit", function (event) {
    var valid = true;
    rupiahFields(event.target).forEach(function (field) {
      formatRupiah(field);
      if (validateRupiah(field) === null) {
        valid = false;
      }
    });
    if (!valid) {
      event.preventDefault();
      event.target.reportValidity();
    }
  }, true);

  document.addEventListener("formdata", function (event) {
    normalizeParameters(event.formData, event.target);
  });
  document.addEventListener("htmx:configRequest", function (event) {
    var source = event.detail && event.detail.elt;
    var form = source && source.closest ? source.closest("form") : source;
    normalizeParameters(event.detail && event.detail.parameters, form);
  });
  document.addEventListener("htmx:afterRequest", function (event) {
    var source = event.detail && event.detail.elt;
    var form = source && source.closest ? source.closest("form") : source;
    rupiahFields(form).forEach(formatRupiah);
  });
  document.addEventListener("DOMContentLoaded", function () {
    enhanceRupiahInputs(document);
    enhanceLoanFeePreviews(document);
  });
  document.addEventListener("htmx:load", function (event) {
    enhanceRupiahInputs(event.detail && event.detail.elt);
    enhanceLoanFeePreviews(event.detail && event.detail.elt);
  });

  window.KopdesRupiah = {
    digits: rupiahDigits,
    format: formatRupiah,
    validate: validateRupiah,
    updateLoanFeePreview: updateLoanFeePreview
  };
}());
