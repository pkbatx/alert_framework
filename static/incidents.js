// Shared incident helpers for formatting, normalization, and UI-friendly view models.

/**
 * @typedef {Object} Incident
 * @property {string} id
 * @property {string} incidentId
 * @property {string} agency
 * @property {string} callType
 * @property {'ems'|'fire'|'other'} callCategory
 * @property {string|null} addressLine
 * @property {string|null} crossStreet
 * @property {string|null} cityOrTown
 * @property {string|null} county
 * @property {string|null} state
 * @property {string|null} summary
 * @property {string[]} tags
 * @property {string|null} timestampLocal
 * @property {string|null} audioPath
 * @property {string|null} audioFilename
 * @property {string|null} audioUrl
 * @property {string} status
 * @property {string[]} missingFields
 * @property {any[]} segments
 * @property {any} location
 */

function coerceID(value, fallback) {
  if (value === undefined || value === null) return fallback;
  const str = String(value).trim();
  return str || fallback;
}

export function deriveCallCategory(callType) {
  const t = (callType || '').toLowerCase();
  if (t.includes('ems') || t.includes('medic') || t.includes('medical')) return 'ems';
  if (t.includes('fire') || t.includes('burning') || t.includes('smoke')) return 'fire';
  return 'other';
}

export function formatIncidentHeader(incident) {
  if (!incident) return '';
  const agency = incident.agency || 'Unknown agency';
  const callType = incident.callType || 'Call';
  const ts = incident.timestampLocal ? new Date(incident.timestampLocal) : null;
  if (!ts || Number.isNaN(ts.getTime())) {
    return `${agency} – ${callType}`;
  }
  const time = ts.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const date = `${ts.getMonth() + 1}/${ts.getDate()}/${ts.getFullYear()}`;
  return `${agency} – ${callType} at ${time} on ${date}`;
}

export function formatIncidentLocation(incident) {
  if (!incident) return 'Location unavailable';
  const parts = [];
  if (incident.addressLine) {
    let base = incident.addressLine;
    if (incident.crossStreet) {
      base += ` (x-street ${incident.crossStreet})`;
    }
    parts.push(base);
  }
  if (incident.cityOrTown) parts.push(incident.cityOrTown);
  if (incident.county) parts.push(`${incident.county} County`);
  if (incident.state) parts.push(incident.state);
  if (!parts.length) return 'Location unavailable';
  return parts.join(', ');
}

function normalizeTags(raw) {
  if (!Array.isArray(raw)) return [];
  return raw.map((t) => String(t || '').trim()).filter(Boolean);
}

function buildAudioPath(raw) {
  if (raw.audio_path) return raw.audio_path;
  if (raw.audio_url) return raw.audio_url;
  if (raw.filename) return `/${encodeURIComponent(raw.filename)}`;
  return null;
}

export function normalizeIncident(raw) {
  const callType = (raw.call_type || raw.normalized_call_type || '').trim();
  const callCategory = (raw.call_category || deriveCallCategory(callType)) || 'other';
  const agency = (raw.primary_agency || raw.agency || raw.town || '').trim();
  const cityOrTown = (raw.city_or_town || raw.town || '').trim() || null;
  const timestampLocal = raw.timestamp_local || raw.call_timestamp || raw.updated_at || raw.created_at || null;
  const tags = normalizeTags(raw.tags);
  const audioPath = buildAudioPath(raw);
  const audioFilename = raw.audio_filename || raw.filename || null;
  const summarySource =
    raw.summary ||
    raw.clean_summary ||
    raw.clean_transcript_text ||
    raw.raw_transcript_text ||
    raw.transcript_text ||
    '';
  const summary = summarySource ? summarySource.trim() : '';

  const missingFields = [];
  if (!agency) missingFields.push('agency');
  if (!callType) missingFields.push('callType');
  if (!timestampLocal) missingFields.push('timestampLocal');

  const id = coerceID(raw.incident_id, coerceID(raw.id, raw.filename || audioFilename || 'unknown'));

  const incident = {
    id,
    incidentId: id,
    filename: raw.filename || raw.audio_filename || audioFilename || id,
    agency: agency || 'Unknown agency',
    primaryAgency: raw.primary_agency || agency || '',
    callType: callType || 'Unknown',
    normalizedCallType: callType || '',
    callCategory,
    addressLine: raw.address_line || null,
    crossStreet: raw.cross_street || null,
    cityOrTown,
    county: raw.county || null,
    state: raw.state || null,
    summary: summary || null,
    tags,
    timestampLocal,
    callTimestamp: timestampLocal,
    audioPath,
    audioFilename,
    audioUrl: raw.audio_url || audioPath || null,
    status: raw.status || 'unknown',
    prettyTitle: raw.pretty_title || raw.filename || '',
    duplicate_of: raw.duplicate_of || raw.duplicateOf || null,
    duplicateOf: raw.duplicateOf || raw.duplicate_of || null,
    location: raw.location || null,
    segments: Array.isArray(raw.segments) ? raw.segments : [],
    cleanTranscript: raw.clean_transcript_text || '',
    rawTranscript: raw.raw_transcript_text || '',
    translation: raw.translation_text || '',
    lastError: raw.last_error || null,
    missingFields,
  };

  // Legacy aliases for existing UI expectations.
  incident.call_type = incident.callType;
  incident.call_category = incident.callCategory;
  incident.normalized_call_type = incident.normalizedCallType;
  incident.address_line = incident.addressLine;
  incident.cross_street = incident.crossStreet;
  incident.city_or_town = incident.cityOrTown;
  incident.audio_path = incident.audioPath;
  incident.audio_filename = incident.audioFilename;
  incident.audio_url = incident.audioUrl;
  incident.call_timestamp = incident.callTimestamp;
  incident.timestamp_local = incident.timestampLocal;
  incident.tags = incident.tags || [];
  incident.town = incident.cityOrTown;
  incident.agency = incident.agency;
  incident.summary = incident.summary;

  if (missingFields.length && incident.id) {
    console.warn('Incident metadata incomplete', { id: incident.id, missing: missingFields });
  }

  return incident;
}

export function formatIncidentSubtitle(incident) {
  if (!incident) return 'Unspecified';
  const ts = incident.timestampLocal ? new Date(incident.timestampLocal) : null;
  if (!ts || Number.isNaN(ts.getTime())) {
    return incident.cityOrTown ? `Time unavailable • ${incident.cityOrTown}` : 'Time unavailable';
  }
  const time = ts.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const date = `${ts.getMonth() + 1}/${ts.getDate()}/${ts.getFullYear()}`;
  const base = `${time} on ${date}`;
  if (incident.cityOrTown) {
    return `${base} • ${incident.cityOrTown}`;
  }
  return base;
}

export function buildIncidentViewModel(incident) {
  const category = incident?.callCategory || 'other';
  const header = `${incident?.agency || 'Unknown agency'} – ${incident?.callType || 'Call'}`;
  const location = formatIncidentLocation(incident);
  const subtitle = formatIncidentSubtitle(incident);
  const summary =
    (incident?.summary || incident?.cleanTranscript || incident?.rawTranscript || '').trim() || 'No summary available.';
  return {
    header,
    location,
    subtitle,
    summary,
    category,
    cardClass: `incident-card incident-card--${category}`,
    audioClass: `incident-audio incident-audio--${category}`,
  };
}
