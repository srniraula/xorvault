class ChunkedUploader {
    static CONCURRENCY_WINDOW = 5;  // Upload 5 chunks in parallel

    constructor(file, apiBase, authToken, chunkSize = 1024 * 1024) { // 1MB chunks
        this.file = file;
        this.apiBase = apiBase;
        this.authToken = authToken;
        this.chunkSize = chunkSize;
        this.totalChunks = Math.ceil(file.size / chunkSize);
        this.uploadId = this.generateUploadId();
        this.onProgress = null;
        this.onError = null;
        this.onComplete = null;
        this.completedChunks = 0;  // Track completed chunks for progress
    }

    generateUploadId() {
        return `upload_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    }

    getAuthHeaders() {
        return {
            'Authorization': `Bearer ${this.authToken}`
        };
    }

    async upload(filename) {
        try {
            // Create array of chunk indices [0, 1, 2, ..., totalChunks-1]
            const chunkIndices = Array.from({ length: this.totalChunks }, (_, i) => i);
            
            // Execute concurrent uploads with sliding window
            await this.executeWithConcurrency(
                chunkIndices,
                (chunkIndex) => this.uploadChunk(chunkIndex, filename),
                ChunkedUploader.CONCURRENCY_WINDOW
            );
            
            // Finalize upload
            const result = await this.finalizeUpload(filename);
            
            if (this.onComplete) {
                this.onComplete(result);
            }
            
            return result;
            
        } catch (error) {
            if (this.onError) {
                this.onError(error);
            }
            throw error;
        }
    }

    async uploadChunk(chunkIndex, filename) {
        const start = chunkIndex * this.chunkSize;
        const end = Math.min(start + this.chunkSize, this.file.size);
        const chunk = this.file.slice(start, end);
        
        const formData = new FormData();
        formData.append('chunk', chunk);
        formData.append('chunkIndex', chunkIndex.toString());
        formData.append('uploadId', this.uploadId);
        formData.append('totalChunks', this.totalChunks.toString());
        formData.append('filename', filename || this.file.name);
        
        const response = await fetch(`${this.apiBase}/files/chunk`, {
            method: 'POST',
            headers: this.getAuthHeaders(),
            body: formData
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.message || `Chunk ${chunkIndex} upload failed`);
        }
        
        return await response.json();
    }

    async executeWithConcurrency(indices, asyncFn, windowSize) {
        const inFlight = new Map();  // Map of index -> promise
        const queue = [...indices];
        
        while (queue.length > 0 || inFlight.size > 0) {
            // Fill the window with new tasks
            while (inFlight.size < windowSize && queue.length > 0) {
                const idx = queue.shift();
                
                // Wrap the promise to capture completion and handle errors
                const promise = asyncFn(idx)
                    .then((result) => {
                        inFlight.delete(idx);
                        this.completedChunks++;
                        this.updateProgress();
                        return result;
                    })
                    .catch((error) => {
                        inFlight.delete(idx);
                        throw { chunkIdx: idx, error };
                    });
                
                inFlight.set(idx, promise);
            }
            
            // If no tasks in flight, we're done
            if (inFlight.size === 0) break;
            
            // Wait for at least one task to complete
            try {
                await Promise.race([...inFlight.values()]);
            } catch (e) {
                // Error occurred—clean up all in-flight promises and propagate
                inFlight.clear();
                throw new Error(`Chunk ${e.chunkIdx} upload failed: ${e.error.message}`);
            }
        }
    }

    updateProgress() {
        if (this.onProgress) {
            const progress = Math.round((this.completedChunks / this.totalChunks) * 100);
            this.onProgress(progress);
        }
    }

    async finalizeUpload(filename) {
        const formData = new FormData();
        formData.append('uploadId', this.uploadId);
        formData.append('filename', filename || this.file.name);
        formData.append('totalSize', this.file.size.toString());
        
        const response = await fetch(`${this.apiBase}/files/finalize`, {
            method: 'POST',
            headers: this.getAuthHeaders(),
            body: formData
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.message || 'Failed to finalize upload');
        }
        
        return await response.json();
    }

    async getUploadStatus() {
        const response = await fetch(`${this.apiBase}/files/status/${this.uploadId}`, {
            headers: this.getAuthHeaders()
        });
        
        if (!response.ok) {
            return { uploadedChunks: [] };
        }
        
        return await response.json();
    }
}

export default ChunkedUploader;