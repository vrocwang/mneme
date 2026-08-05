import { configureStore, combineReducers } from '@reduxjs/toolkit';
import { persistStore, persistReducer } from 'redux-persist';
import storage from 'redux-persist/lib/storage';
import chatReducer from './chatSlice';
import threadReducer from './threadSlice';
import approvalReducer from './approvalSlice';
import chatRuntimeReducer from './chatRuntimeSlice';

const rootReducer = combineReducers({
  chat: chatReducer,
  thread: threadReducer,
  approval: approvalReducer,
  chatRuntime: chatRuntimeReducer,
});

const persistConfig = {
  key: 'mneme',
  storage,
  version: 2,
  whitelist: ['thread'],
  migrate: (state: any) => {
    if (state) {
      // Remove old slice keys that no longer exist (v1 → v2 cleanup)
      delete state.settings;
      delete state.theme;
      delete state.notification;
      delete state.connectivity;
    }
    return Promise.resolve(state);
  },
};

const persistedReducer = persistReducer(persistConfig, rootReducer);

export const store = configureStore({
  reducer: persistedReducer,
  middleware: (getDefaultMiddleware) =>
    getDefaultMiddleware({
      serializableCheck: {
        ignoredActions: ['persist/PERSIST', 'persist/REHYDRATE'],
      },
    }),
});

export const persistor = persistStore(store);

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;
