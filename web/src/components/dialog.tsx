import { useState } from 'react'

interface DialogProps {
  title: string
  message: string
  onClose: () => void
}

export function Dialog({ title, message, onClose }: DialogProps) {
  return (
    <div className="fixed inset-0 bg-black/20 backdrop-blur-sm flex items-center justify-center z-50">
      <div className="bg-white rounded-xl shadow-xl max-w-md w-full mx-4 overflow-hidden">
        <div className="px-6 py-4 bg-macaron-rose/30 border-b border-macaron-rose/50">
          <h3 className="text-lg font-medium text-gray-800">{title}</h3>
        </div>
        <div className="px-6 py-4">
          <p className="text-sm text-gray-600 whitespace-pre-wrap">{message}</p>
        </div>
        <div className="px-6 py-3 bg-gray-50 flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-1.5 text-sm bg-macaron-orange/80 hover:bg-macaron-orange text-white rounded-lg transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

export function useDialog() {
  const [dialog, setDialog] = useState<{ title: string; message: string } | null>(null)

  const showDialog = (title: string, message: string) => {
    setDialog({ title, message })
  }

  return { dialog, showDialog }
}