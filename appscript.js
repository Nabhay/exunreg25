/**
 * Google Apps Script for the Users sheet.
 * - Adds a custom menu to set the webhook secret and install the onEdit trigger
 * - Ensures headers are initialized from the server
 * - Sends edited rows to the backend webhook immediately
 *
 * Fill in BASE_URL below with your backend server origin.
 */

// TODO: set this to your server origin (no trailing slash), e.g. "https://exun.example.com"
const BASE_URL = "https://YOUR_SERVER_HOST";

const SHEET_NAME = "users"; // keep in sync with backend
const SCRIPT_PROP_SECRET_KEY = "SHEETS_WEBHOOK_SECRET";

function onOpen() {
  const ui = SpreadsheetApp.getUi();
  ui.createMenu("Exun Reg")
    .addItem("Setup: Install Trigger", "setupScript")
    .addItem("Initialize Headers", "initializeHeaders")
    .addItem("Send Current Row", "sendActiveRowNow")
    .addToUi();
}

/**
 * One-time setup: set secret and install an installable onEdit trigger.
 */
function setupScript() {
  ensureBaseUrlConfigured_();
  // Secret is optional; trigger works without it
  installOnEditTrigger_();
  try { SpreadsheetApp.getUi().alert("Setup complete. Trigger installed."); } catch (e) {}
}

function ensureBaseUrlConfigured_() {
  if (!BASE_URL || BASE_URL.indexOf("http") !== 0 || BASE_URL.endsWith("YOUR_SERVER_HOST")) {
    SpreadsheetApp.getUi().alert("Please edit appscript.js and set BASE_URL to your server origin before running setup.");
    throw new Error("BASE_URL not configured");
  }
}

function setSecret_() {
  const ui = SpreadsheetApp.getUi();
  const resp = ui.prompt("Enter SHEETS_WEBHOOK_SECRET", "Paste the exact secret configured in your server env", ui.ButtonSet.OK_CANCEL);
  if (resp.getSelectedButton() !== ui.Button.OK) {
    throw new Error("Secret not set");
  }
  const secret = (resp.getResponseText() || "").trim();
  if (!secret) {
    throw new Error("Empty secret");
  }
  PropertiesService.getScriptProperties().setProperty(SCRIPT_PROP_SECRET_KEY, secret);
}

function installOnEditTrigger_() {
  // Remove existing triggers for onEdit to avoid duplicates
  const triggers = ScriptApp.getProjectTriggers();
  for (var i = 0; i < triggers.length; i++) {
    if (triggers[i].getHandlerFunction() === "onEditHandler") {
      ScriptApp.deleteTrigger(triggers[i]);
    }
  }
  ScriptApp.newTrigger("onEditHandler").forSpreadsheet(SpreadsheetApp.getActive()).onEdit().create();
}

/**
 * Installable onEdit trigger handler. Simple triggers cannot use UrlFetchApp, so we use an installable one.
 */
function onEditHandler(e) {
  try {
    ensureHeaders_();
    const sheet = e.range.getSheet();
    if (sheet.getName() !== SHEET_NAME) return; // ignore other sheets
    if (e.range.getRow() === 1) return; // ignore header edits

    const rowIndex = e.range.getRow();
    const headers = getHeaders_();
    if (!headers || headers.length === 0) return;

    const row = getRowValuesAsStrings_(sheet, rowIndex, headers.length);
    postRowUpdate_(headers, row);
  } catch (err) {
    Logger.log("onEditHandler error: " + err);
  }
}

/**
 * Menu action: initialize headers by fetching from server and writing into row 1.
 */
function initializeHeaders() {
  ensureHeaders_(true);
}

/**
 * Menu action: send the currently active row to the server (useful for manual retries).
 */
function sendActiveRowNow() {
  const sheet = SpreadsheetApp.getActiveSheet();
  if (sheet.getName() !== SHEET_NAME) {
    SpreadsheetApp.getUi().alert("Active sheet is not '" + SHEET_NAME + "'");
    return;
  }
  const rowIndex = sheet.getActiveCell().getRow();
  if (rowIndex <= 1) {
    SpreadsheetApp.getUi().alert("Please select a data row (row >= 2)");
    return;
  }
  const headers = getHeaders_();
  if (!headers || headers.length === 0) {
    SpreadsheetApp.getUi().alert("Headers not initialized yet");
    return;
  }
  const row = getRowValuesAsStrings_(sheet, rowIndex, headers.length);
  try {
    postRowUpdate_(headers, row);
    SpreadsheetApp.getUi().alert("Row sent");
  } catch (err) {
    SpreadsheetApp.getUi().alert("Send failed: " + err);
  }
}

function ensureHeaders_(forceWrite) {
  const sheet = SpreadsheetApp.getActive().getSheetByName(SHEET_NAME) || SpreadsheetApp.getActive().insertSheet(SHEET_NAME);
  const currentHeaders = sheet.getRange(1, 1, 1, Math.max(sheet.getLastColumn(), 1)).getValues()[0];
  const areHeadersPresent = currentHeaders.some(function (v) { return (v || "").toString().trim() !== ""; });
  if (areHeadersPresent && !forceWrite) return;

  const secret = PropertiesService.getScriptProperties().getProperty(SCRIPT_PROP_SECRET_KEY); // optional
  const url = BASE_URL + "/api/sheets/headers?sheet=" + encodeURIComponent(SHEET_NAME);
  const options = { method: "get", muteHttpExceptions: true };
  if (secret) { options.headers = { Authorization: "Bearer " + secret }; }
  const resp = UrlFetchApp.fetch(url, options);
  const rc = resp.getResponseCode();
  if (rc !== 200) {
    throw new Error("Headers fetch failed: HTTP " + rc + ": " + resp.getContentText());
  }
  const data = JSON.parse(resp.getContentText());
  const headers = data.headers || [];
  if (!headers || headers.length === 0) {
    throw new Error("Empty headers from server");
  }
  // Write headers to row 1
  sheet.clear();
  sheet.getRange(1, 1, 1, headers.length).setValues([headers]);
  sheet.setFrozenRows(1);
}

function getHeaders_() {
  const sheet = SpreadsheetApp.getActive().getSheetByName(SHEET_NAME);
  if (!sheet) return [];
  const lastCol = sheet.getLastColumn();
  if (lastCol < 1) return [];
  return sheet.getRange(1, 1, 1, lastCol).getValues()[0].map(function (v) { return (v || "").toString(); });
}

function getRowValuesAsStrings_(sheet, rowIndex, width) {
  const range = sheet.getRange(rowIndex, 1, 1, width);
  const values = range.getValues()[0];
  const out = [];
  for (var i = 0; i < width; i++) {
    var v = i < values.length ? values[i] : "";
    if (v instanceof Date) {
      out.push(Utilities.formatDate(v, Session.getScriptTimeZone(), "yyyy-MM-dd'T'HH:mm:ssXXX"));
    } else if (v === null || v === undefined) {
      out.push("");
    } else {
      out.push(String(v));
    }
  }
  return out;
}

function postRowUpdate_(headers, row) {
  const secret = PropertiesService.getScriptProperties().getProperty(SCRIPT_PROP_SECRET_KEY); // optional
  const url = BASE_URL + "/api/sheets/webhook";
  const payload = { sheet: SHEET_NAME, pk: "email", headers: headers, row: row };
  const options = { method: "post", contentType: "application/json", payload: JSON.stringify(payload), muteHttpExceptions: true };
  if (secret) { options.headers = { Authorization: "Bearer " + secret }; }
  const resp = UrlFetchApp.fetch(url, options);
  const rc = resp.getResponseCode();
  if (rc !== 200) {
    throw new Error("Webhook failed: HTTP " + rc + ": " + resp.getContentText());
  }
}
