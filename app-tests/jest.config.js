/** Tests live here; the code under test lives in ../app/lib and resolves its
 * own node_modules (zustand, async-storage) from ../app. */
module.exports = {
  rootDir: '..',
  roots: ['<rootDir>/app-tests'],
  testMatch: ['<rootDir>/app-tests/lib/**/*.test.ts'],
  transform: { '^.+\\.tsx?$': [require.resolve('ts-jest'), { tsconfig: '<rootDir>/app-tests/tsconfig.json', diagnostics: false }] },
  moduleNameMapper: {
    '^@app/(.*)$': '<rootDir>/app/lib/$1',
    '^@react-native-async-storage/async-storage$': '<rootDir>/app-tests/mocks/async-storage.ts',
    '^expo-secure-store$': '<rootDir>/app-tests/mocks/secure-store.ts',
  },
  testEnvironment: 'node',
};
