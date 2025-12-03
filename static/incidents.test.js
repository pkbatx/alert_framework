import assert from 'node:assert';
import test from 'node:test';

import {
  buildIncidentViewModel,
  deriveCallCategory,
  formatIncidentHeader,
  formatIncidentLocation,
  normalizeIncident,
} from './incidents.js';

process.env.TZ = 'UTC';

test('deriveCallCategory maps common call types', () => {
  assert.strictEqual(deriveCallCategory('EMS'), 'ems');
  assert.strictEqual(deriveCallCategory('medical emergency'), 'ems');
  assert.strictEqual(deriveCallCategory('Fire Alarm'), 'fire');
  assert.strictEqual(deriveCallCategory('Other'), 'other');
});

test('formatIncidentHeader uses local timestamp when available', () => {
  const incident = {
    agency: 'Sparta EMS',
    callType: 'Medical',
    timestampLocal: '2025-12-03T14:18:20.000Z',
  };
  const header = formatIncidentHeader(incident);
  assert.ok(header.startsWith('Sparta EMS – Medical at'), header);
  assert.ok(header.includes('12/3/2025'), header);
});

test('formatIncidentLocation handles cross streets and fallbacks', () => {
  const incident = {
    addressLine: '5 Pine Cone Lane',
    crossStreet: 'Pine Court',
    cityOrTown: 'Sparta',
    county: 'Sussex',
    state: 'NJ',
  };
  const location = formatIncidentLocation(incident);
  assert.strictEqual(location, '5 Pine Cone Lane (x-street Pine Court), Sparta, Sussex County, NJ');

  const missing = formatIncidentLocation({});
  assert.strictEqual(missing, 'Location unavailable');
});

test('normalizeIncident enforces defaults and missing metadata warnings', () => {
  const incident = normalizeIncident({
    filename: 'Sparta_EMS_2025_12_03_14_18_20.mp3',
    call_type: 'EMS',
    tags: null,
    timestamp_local: '2025-12-03T14:18:20Z',
  });
  assert.strictEqual(incident.callCategory, 'ems');
  assert.deepStrictEqual(incident.tags, []);
  assert.ok(Array.isArray(incident.missingFields));
  assert.strictEqual(incident.audioPath, '/Sparta_EMS_2025_12_03_14_18_20.mp3');
});

test('buildIncidentViewModel returns themed classes and summary', () => {
  const incident = normalizeIncident({
    filename: 'Newton_Fire_2025_01_02_03_04_05.mp3',
    call_type: 'Fire',
    call_category: 'fire',
    agency: 'Newton FD',
    timestamp_local: '2025-01-02T03:04:05Z',
    summary: 'Structure fire with smoke showing.',
  });
  const vm = buildIncidentViewModel(incident);
  assert.strictEqual(vm.category, 'fire');
  assert.ok(vm.cardClass.includes('incident-card--fire'));
  assert.ok(vm.audioClass.includes('incident-audio--fire'));
  assert.ok(vm.summary.includes('Structure fire'));
});
