import { configureStore, createSlice, type PayloadAction } from '@reduxjs/toolkit'

type Density = 'comfortable' | 'compact'

interface UiState {
  density: Density
  paletteOpen: boolean
}

const ui = createSlice({
  name: 'ui',
  initialState: { density: 'comfortable', paletteOpen: false } as UiState,
  reducers: {
    setDensity(s, a: PayloadAction<Density>) {
      s.density = a.payload
    },
    setPaletteOpen(s, a: PayloadAction<boolean>) {
      s.paletteOpen = a.payload
    },
  },
})

export const { setDensity, setPaletteOpen } = ui.actions

export const store = configureStore({
  reducer: { ui: ui.reducer },
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch
