class ChunkedUploader {
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
            for (let chunkIndex = 0; chunkIndex < this.totalChunks; chunkIndex++) {
                await this.uploadChunk(chunkIndex, filename);
                
                // Update progress
                const progress = Math.round(((chunkIndex + 1) / this.totalChunks) * 100);
                if (this.onProgress) {
                    this.onProgress(progress);
                }
            }
            
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